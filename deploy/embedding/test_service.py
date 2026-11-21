import contextlib
import dataclasses
import http.client
import json
import math
import socket
import threading
import time
import unittest
from unittest.mock import patch

import service

REVISION = "0123456789abcdef0123456789abcdef01234567"


class FakeModel:
    revision = REVISION

    def embed(self, texts):
        self.texts = texts
        return [[2.0] + [0.0] * 1023 for _ in texts]


@contextlib.contextmanager
def running(model=None, **timeouts):
    config = service.Config(REVISION, "test-secret", cache_dir="/unused")
    server = service.EmbeddingServer(("127.0.0.1", 0), config)
    for name, value in timeouts.items():
        setattr(server, name, value)
    server.model = model or FakeModel()
    server.ready.set()
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def request(server, payload=None, headers=None, method="POST", path="/v1/embeddings"):
    conn = http.client.HTTPConnection(*server.server_address, timeout=2)
    body = json.dumps(payload or {"model": service.MODEL, "input": [" query ", "document"]})
    defaults = {"Authorization": "Bearer test-secret", "X-Embedding-Revision": REVISION, "Content-Type": "application/json"}
    if headers:
        defaults.update(headers)
    conn.request(method, path, body, defaults)
    response = conn.getresponse()
    result = response.status, dict(response.getheaders()), json.loads(response.read())
    conn.close()
    return result


class ServiceTests(unittest.TestCase):
    def test_contract(self):
        model = FakeModel()
        with running(model) as server:
            status, headers, body = request(server)
            self.assertEqual(status, 200)
            self.assertEqual(headers["X-Embedding-Revision"], REVISION)
            self.assertEqual(model.texts, [" query ", "document"])
            self.assertEqual(body["model"], service.MODEL)
            self.assertEqual(len(body["data"]), 2)
            for index, item in enumerate(body["data"]):
                self.assertEqual(item["index"], index)
                self.assertEqual(len(item["embedding"]), 1024)
                self.assertAlmostEqual(math.hypot(*item["embedding"]), 1)

    def test_auth_revision_readiness_and_limits(self):
        with running() as server:
            for headers, status in [
                ({"Authorization": ""}, 401),
                ({"Authorization": "Bearer wrong"}, 401),
                ({"X-Embedding-Revision": ""}, 409),
                ({"X-Embedding-Revision": "a" * 40}, 409),
                ({"Transfer-Encoding": "chunked"}, 400),
                ({"Content-Length": str(service.MAX_BODY_BYTES + 1)}, 413),
            ]:
                self.assertEqual(request(server, headers=headers)[0], status)
            server.ready.clear()
            self.assertEqual(request(server)[0], 503)
            self.assertEqual(request(server, method="GET", path="/health")[0], 503)
            server.ready.set()
            self.assertEqual(request(server, method="GET", path="/health")[0], 200)
            self.assertEqual(request(server, path="/v1/chat/completions")[0], 404)
            server.inference_slots.acquire()
            try:
                self.assertEqual(request(server)[0], 503)
            finally:
                server.inference_slots.release()

    def test_slow_drip_releases_slots_and_health_recovers(self):
        headers = ("POST /v1/embeddings HTTP/1.0\r\n"
                   "Authorization: Bearer test-secret\r\n"
                   f"X-Embedding-Revision: {REVISION}\r\n"
                   "Content-Type: application/json\r\nContent-Length: 1000\r\n\r\n").encode()
        for phase, prefix, count in [
            ("request_line", b"GET /", 9),
            ("headers", b"GET /health HTTP/1.0\r\nX-Slow: ", 9),
            ("connection", b"GET /health HTTP/1.0\r\nX-Slow: ", 9),
            ("body", headers, 1),
        ]:
            with self.subTest(phase=phase), running(
                    header_timeout=0.6, body_timeout=0.6,
                    connection_timeout=0.3 if phase == "connection" else 3) as server:
                stopped = threading.Event()
                clients = []
                worker = None

                def drip():
                    while not stopped.wait(0.03):
                        for client in clients:
                            try:
                                client.sendall(b"x")
                            except OSError:
                                pass
                try:
                    for _ in range(count):
                        client = socket.create_connection(server.server_address, timeout=1)
                        clients.append(client)
                        client.sendall(prefix)
                    until = time.monotonic() + 1
                    while len(server.deadlines) != count and time.monotonic() < until:
                        stopped.wait(0.005)
                    self.assertEqual(len(server.deadlines), count)
                    worker = threading.Thread(target=drip)
                    worker.start()
                    if count == 9:
                        conn = http.client.HTTPConnection(*server.server_address, timeout=1)
                        conn.request("GET", "/health")
                        response = conn.getresponse()
                        self.assertEqual(response.status, 503)
                        response.read()
                        conn.close()
                    # The peer stays open and keeps sending well inside the 10s
                    # inactivity timeout; only an absolute deadline frees slots.
                    until = time.monotonic() + 1.5
                    while server.deadlines and time.monotonic() < until:
                        stopped.wait(0.01)
                    self.assertFalse(server.deadlines)
                    self.assertEqual(request(server, method="GET", path="/health")[0], 200)
                    # Receiving the response precedes server-thread cleanup.
                    until = time.monotonic() + 1
                    while server.deadlines and time.monotonic() < until:
                        stopped.wait(0.005)
                    self.assertFalse(server.deadlines)
                    for _ in range(9):
                        self.assertTrue(server.connection_slots.acquire(blocking=False))
                    for _ in range(9):
                        server.connection_slots.release()
                    self.assertTrue(server.inference_slots.acquire(blocking=False))
                    server.inference_slots.release()
                    self.assertFalse(hasattr(server.model, "texts"))
                finally:
                    stopped.set()
                    if worker is not None:
                        worker.join()
                    for client in clients:
                        client.close()

    def test_upload_deadlines_do_not_interrupt_inference(self):
        class SlowModel(FakeModel):
            def embed(self, texts):
                # Longer than both upload budgets, but inside the total SLA.
                threading.Event().wait(0.4)
                return super().embed(texts)

        with running(SlowModel(), header_timeout=0.15, body_timeout=0.15,
                     connection_timeout=2) as server:
            self.assertEqual(request(server)[0], 200)
            self.assertEqual(request(server, method="GET", path="/health")[0], 200)

    def test_deadline_cancellation_and_stale_callbacks(self):
        client, peer = socket.socketpair()
        with client, peer:
            deadline = service.ConnectionDeadline(client, 2, 0.2)
            try:
                header_timer = deadline.timer
                deadline.set_phase(0.2)
                body_timer = deadline.timer
                deadline.set_phase()
                total_timer = deadline.timer
                deadline.expire(header_timer)
                deadline.expire(body_timer)
                self.assertFalse(deadline.expired)
                deadline.cancel()
                deadline.expire(total_timer)
                self.assertFalse(deadline.expired)
                for timer in (header_timer, body_timer, total_timer):
                    timer.join(timeout=1)
                    self.assertFalse(timer.is_alive())
                    self.assertTrue(timer.finished.is_set())
            finally:
                deadline.cancel()

    def test_connection_completion_cancels_timer(self):
        deadlines = []
        original = service.ConnectionDeadline

        def track(*args):
            deadline = original(*args)
            deadlines.append(deadline)
            return deadline

        with patch.object(service, "ConnectionDeadline", side_effect=track):
            with running() as server:
                self.assertEqual(request(server)[0], 200)
                until = time.monotonic() + 1
                while server.deadlines and time.monotonic() < until:
                    threading.Event().wait(0.005)
                self.assertFalse(server.deadlines)
                self.assertEqual(len(deadlines), 1)
                self.assertIsNone(deadlines[0].timer)
                self.assertFalse(deadlines[0].expired)

    def test_validation(self):
        for texts in [[], [" "], ["x"] * 33, [1], ["x" * 16385]]:
            with self.assertRaises(service.InvalidInput):
                service.parse_input(json.dumps({"model": service.MODEL, "input": texts}).encode())
        for extra in [{"dimensions": 768}, {"dimensions": True}, {"encoding_format": "base64"}, {"revision": REVISION}]:
            with self.assertRaises(service.InvalidInput):
                service.parse_input(json.dumps({"model": service.MODEL, "input": ["x"], **extra}).encode())
        for body in [b"{", b"\xff", b"x" * (service.MAX_BODY_BYTES + 1)]:
            with self.assertRaises(service.InvalidInput):
                service.parse_input(body)
        for vector in [[0] * 1024, [1] * 768, [float("nan")] * 1024, [float("inf")] * 1024]:
            with self.assertRaises(RuntimeError):
                service.response_data([vector], 1)

    def test_pinned_load_and_tokenizer_no_truncation(self):
        import sys
        from types import ModuleType, SimpleNamespace
        from unittest.mock import Mock

        class Tensor:
            def to(self, device):
                return self
            def detach(self):
                return self
            def cpu(self):
                return self
            def tolist(self):
                return [[1.0] + [0.0] * 1023]

        tokenizer = Mock()
        tokenizer.return_value = {"input_ids": [[1] * 8192], "attention_mask": [[1] * 8192]}
        tokenizer.pad.return_value = {"input_ids": Tensor(), "attention_mask": Tensor()}
        model = Mock()
        model.tokenizer = tokenizer
        model.device = "cpu"
        model.get_sentence_embedding_dimension.return_value = 1024
        model.return_value = {"sentence_embedding": Tensor()}
        torch = ModuleType("torch")
        torch.inference_mode = contextlib.nullcontext
        hub = ModuleType("huggingface_hub")
        hub.snapshot_download = Mock(return_value="/cache/snapshots/" + REVISION)
        transformers = ModuleType("sentence_transformers")
        transformers.SentenceTransformer = Mock(return_value=model)
        with patch.dict(sys.modules, {"torch": torch, "huggingface_hub": hub, "sentence_transformers": transformers}):
            dense = service.DenseModel(service.Config(REVISION, "secret", cache_dir="/cache"))
            hub.snapshot_download.assert_called_once_with(repo_id=service.MODEL, revision=REVISION, cache_dir="/cache", local_files_only=True)
            kwargs = transformers.SentenceTransformer.call_args.kwargs
            self.assertFalse(kwargs["trust_remote_code"])
            self.assertTrue(kwargs["local_files_only"])
            self.assertTrue(kwargs["model_kwargs"]["use_safetensors"])
            self.assertEqual(dense.revision, REVISION)
            self.assertEqual(len(dense.embed([" query "])[0]), 1024)
            tokenizer.assert_called_once_with([" query "], truncation=False, padding=False, add_special_tokens=True, return_attention_mask=True)
            tokenizer.return_value = {"input_ids": [[1] * 8193]}
            with self.assertRaises(service.InvalidInput):
                dense.embed(["too many tokens"])
            self.assertEqual(model.call_count, 1)
            hub.snapshot_download.return_value = "/cache/snapshots/" + "a" * 40
            with self.assertRaises(RuntimeError):
                service.DenseModel(service.Config(REVISION, "secret", cache_dir="/cache"))

    def test_model_failures_are_redacted(self):
        model = FakeModel()
        model.embed = lambda texts: (_ for _ in ()).throw(RuntimeError("secret input"))
        with running(model) as server:
            status, _, body = request(server)
            self.assertEqual(status, 500)
            self.assertNotIn("secret", json.dumps(body))

    def test_immutable_config(self):
        config = service.Config(REVISION, "secret", cache_dir="/unused")
        with self.assertRaises(dataclasses.FrozenInstanceError):
            config.revision = "main"
        for revision, key, concurrency in [("main", "secret", 1), (REVISION, "", 1), (REVISION, "bad\n", 1), (REVISION, "secret", 0)]:
            with self.assertRaises(ValueError):
                service.Config(revision, key, concurrency=concurrency, cache_dir="/unused")
