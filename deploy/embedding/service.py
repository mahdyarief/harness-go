"""Private, offline BGE-M3 dense embedding HTTP service."""

import hmac
import json
import logging
import math
import os
import re
import socket
import threading
import time
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL = "BAAI/bge-m3"
DIMENSIONS = 1024
MAX_TOKENS = 8192
MAX_BATCH = 32
MAX_ITEM_BYTES = 16 * 1024
MAX_BODY_BYTES = 128 * 1024


@dataclass(frozen=True)
class Config:
    revision: str
    api_key: str
    concurrency: int = 1
    cache_dir: str = "/models"

    def __post_init__(self):
        if not re.fullmatch(r"[0-9a-f]{40}", self.revision):
            raise ValueError("Revision must be a lowercase 40-hex commit")
        if not self.api_key or not self.api_key.isascii() or any(ord(c) <= 32 or ord(c) >= 127 for c in self.api_key):
            raise ValueError("API key must be a nonempty printable ASCII token")
        if type(self.concurrency) is not int or not 1 <= self.concurrency <= 8:
            raise ValueError("Concurrency must be between 1 and 8")
        if not self.cache_dir:
            raise ValueError("Model cache is required")

    @classmethod
    def from_env(cls):
        revision = os.environ.get("EMBEDDING_MODEL_REVISION", "")
        key = os.environ.get("EMBEDDING_API_KEY", "")
        if not re.fullmatch(r"[0-9a-f]{40}", revision):
            raise ValueError("EMBEDDING_MODEL_REVISION must be a lowercase 40-hex commit")
        if not key or not key.isascii() or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise ValueError("EMBEDDING_API_KEY must be a nonempty printable ASCII token")
        concurrency = int(os.environ.get("EMBEDDING_MAX_CONCURRENCY", "1"))
        if not 1 <= concurrency <= 8:
            raise ValueError("EMBEDDING_MAX_CONCURRENCY must be between 1 and 8")
        return cls(revision, key, concurrency, os.environ.get("EMBEDDING_CACHE_DIR", "/models"))


class InvalidInput(Exception):
    pass


class DenseModel:
    def __init__(self, config):
        import torch
        from pathlib import Path
        from huggingface_hub import snapshot_download
        from sentence_transformers import SentenceTransformer

        snapshot = snapshot_download(
            repo_id=MODEL, revision=config.revision,
            cache_dir=config.cache_dir, local_files_only=True,
        )
        if Path(snapshot).name != config.revision:
            raise RuntimeError("Model snapshot revision mismatch")
        self.revision = config.revision
        self.torch = torch
        self.model = SentenceTransformer(
            snapshot, revision=config.revision, trust_remote_code=False,
            local_files_only=True, cache_folder=config.cache_dir, device="cpu",
        )
        if self.model.get_sentence_embedding_dimension() != DIMENSIONS:
            raise RuntimeError("Invalid model dimensions")
        self.model.eval()

    def embed(self, texts):
        # Do not use encode(): its tokenizer silently truncates long inputs.
        tokens = self.model.tokenizer(
            texts, truncation=False, padding=False, add_special_tokens=True,
            return_attention_mask=True,
        )
        if any(len(ids) > MAX_TOKENS for ids in tokens["input_ids"]):
            raise InvalidInput()
        features = self.model.tokenizer.pad(tokens, padding=True, return_tensors="pt")
        features = {name: value.to(self.model.device) for name, value in features.items()}
        with self.torch.inference_mode():
            return self.model(features)["sentence_embedding"].detach().cpu().tolist()


def parse_input(body):
    if not 0 < len(body) <= MAX_BODY_BYTES:
        raise InvalidInput()
    try:
        request = json.loads(body.decode("utf-8"))
        if not isinstance(request, dict) or request.get("model") != MODEL:
            raise InvalidInput()
        if set(request) - {"model", "input", "encoding_format", "dimensions"}:
            raise InvalidInput()
        if request.get("encoding_format", "float") != "float":
            raise InvalidInput()
        if "dimensions" in request and (type(request["dimensions"]) is not int or request["dimensions"] != DIMENSIONS):
            raise InvalidInput()
        texts = request.get("input")
        if not isinstance(texts, list) or not 1 <= len(texts) <= MAX_BATCH:
            raise InvalidInput()
        if any(not isinstance(text, str) or not text.strip() or len(text.encode("utf-8")) > MAX_ITEM_BYTES for text in texts):
            raise InvalidInput()
        return texts
    except (ValueError, UnicodeError, RecursionError) as exc:
        raise InvalidInput() from exc


def response_data(vectors, count):
    if len(vectors) != count:
        raise RuntimeError("Invalid model output")
    data = []
    for index, vector in enumerate(vectors):
        if len(vector) != DIMENSIONS:
            raise RuntimeError("Invalid model output")
        if any(type(value) not in (float, int) or not math.isfinite(value) for value in vector):
            raise RuntimeError("Invalid model output")
        norm = math.hypot(*vector)
        if not math.isfinite(norm) or norm == 0:
            raise RuntimeError("Invalid model output")
        data.append({"object": "embedding", "index": index,
                     "embedding": [value / norm for value in vector]})
    return {"object": "list", "model": MODEL, "data": data}


class ConnectionDeadline:
    """One absolute connection budget, with shorter header/body read budgets."""

    def __init__(self, request, total_timeout, header_timeout):
        self.request = request
        self.lock = threading.Lock()
        self.total = time.monotonic() + total_timeout
        self.expires = self.total
        self.timer = None
        self.expired = False
        self.set_phase(header_timeout)

    def set_phase(self, timeout=None):
        with self.lock:
            now = time.monotonic()
            if self.expired or now >= self.expires:
                raise TimeoutError("Connection deadline exceeded")
            if self.timer is not None:
                self.timer.cancel()
            self.expires = min(self.total, now + timeout) if timeout is not None else self.total
            timer = threading.Timer(self.expires - now, lambda: self.expire(timer))
            # Identity is checked under the lock: a cancelled callback cannot
            # close a connection that has already moved to the next phase.
            timer.daemon = True
            self.timer = timer
            timer.start()

    def expire(self, timer):
        with self.lock:
            if self.timer is not timer:
                return
            self.expired = True
            try:
                # close() alone does not unblock a buffered rfile.read().
                self.request.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass

    def cancel(self):
        with self.lock:
            if self.timer is not None:
                self.timer.cancel()
                self.timer = None


class EmbeddingServer(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 16
    header_timeout = 10
    body_timeout = 10
    # Preserve the client's supported 120s inference SLA plus upload budgets.
    connection_timeout = 140

    def __init__(self, address, config):
        self.config = config
        self.model = None
        self.ready = threading.Event()
        self.inference_slots = threading.BoundedSemaphore(config.concurrency)
        self.connection_slots = threading.BoundedSemaphore(config.concurrency + 8)
        self.deadlines = {}
        super().__init__(address, Handler)

    def load_model(self):
        try:
            self.model = DenseModel(self.config)
            if self.model.revision != self.config.revision:
                raise RuntimeError("Loaded revision mismatch")
            self.ready.set()
        except Exception:
            # Never include model/library exceptions: they may contain input or paths.
            logging.error("Embedding model unavailable")

    def process_request(self, request, client_address):
        request.settimeout(10)
        if not self.connection_slots.acquire(blocking=False):
            try:
                request.sendall(("HTTP/1.0 503 Service Unavailable\r\n"
                                 "Content-Length: 0\r\nConnection: close\r\n"
                                 f"X-Embedding-Revision: {self.config.revision}\r\n\r\n").encode("ascii"))
            except OSError:
                pass
            self.shutdown_request(request)
            return
        try:
            self.deadlines[request] = ConnectionDeadline(
                request, self.connection_timeout, self.header_timeout)
            super().process_request(request, client_address)
        except Exception:
            deadline = self.deadlines.pop(request, None)
            if deadline is not None:
                deadline.cancel()
            self.connection_slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.deadlines.pop(request).cancel()
            self.connection_slots.release()

    def handle_error(self, request, client_address):
        logging.error("Embedding request failed")


class Handler(BaseHTTPRequestHandler):
    server_version = "EmbeddingService"
    sys_version = ""

    def parse_request(self):
        parsed = super().parse_request()
        if parsed:
            self.server.deadlines[self.connection].set_phase()
        return parsed

    def log_message(self, format, *args):
        pass  # No access logs: request targets and headers can contain secrets.

    def reply(self, status, payload):
        body = json.dumps(payload, allow_nan=False, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("X-Embedding-Revision", self.server.config.revision)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        self.wfile.write(body)

    def send_error(self, code, message=None, explain=None):
        self.reply(code, {"error": {"message": "Invalid request"}})

    def fail(self, status, message):
        self.reply(status, {"error": {"message": message}})

    def do_GET(self):
        if self.path != "/health":
            return self.fail(404, "Not found")
        ready = self.server.ready.is_set()
        self.reply(200 if ready else 503, {"status": "ready" if ready else "unavailable"})

    def do_POST(self):
        if self.path != "/v1/embeddings":
            return self.fail(404, "Not found")
        auth = self.headers.get_all("Authorization", [])
        expected = f"Bearer {self.server.config.api_key}".encode("ascii")
        if len(auth) != 1 or not hmac.compare_digest(auth[0].encode("utf-8"), expected):
            return self.fail(401, "Unauthorized")
        revisions = self.headers.get_all("X-Embedding-Revision", [])
        if revisions != [self.server.config.revision]:
            return self.fail(409, "Embedding revision mismatch")
        if not self.server.ready.is_set():
            return self.fail(503, "Model unavailable")
        lengths = self.headers.get_all("Content-Length", [])
        if "Transfer-Encoding" in self.headers or len(lengths) != 1:
            return self.fail(400, "Invalid request framing")
        if not re.fullmatch(r"[0-9]{1,6}", lengths[0]):
            return self.fail(400, "Invalid request framing")
        length = int(lengths[0])
        if length > MAX_BODY_BYTES:
            return self.fail(413, "Request too large")
        if length == 0:
            return self.fail(400, "Invalid request")
        if self.headers.get_content_type() != "application/json" or "Content-Encoding" in self.headers:
            return self.fail(415, "Unsupported content type")
        if not self.server.inference_slots.acquire(blocking=False):
            return self.fail(503, "Service busy")
        try:
            deadline = self.server.deadlines[self.connection]
            deadline.set_phase(self.server.body_timeout)
            body = self.rfile.read(length)
            if len(body) != length:
                return self.fail(400, "Incomplete request")
            # Upload deadlines must not terminate legitimate model inference.
            deadline.set_phase()
            texts = parse_input(body)
            result = response_data(self.server.model.embed(texts), len(texts))
            self.reply(200, result)
        except (socket.timeout, TimeoutError):
            self.fail(408, "Request timeout")
        except InvalidInput:
            self.fail(400, "Invalid input or token limit exceeded")
        except Exception:
            self.fail(500, "Embedding failed")
        finally:
            self.server.inference_slots.release()


def main():
    logging.basicConfig(level=logging.ERROR, format="%(message)s")
    config = Config.from_env()
    server = EmbeddingServer(("0.0.0.0", 8080), config)
    threading.Thread(target=server.load_model, daemon=True).start()
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
