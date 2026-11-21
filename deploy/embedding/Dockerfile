# linux/amd64 CPU runtime, pinned to the Python 3.12.16 image manifest.
FROM python:3.12.16-slim-bookworm@sha256:623674a14ef93d052bb9b0de4dbf5f28eb578a35263838941cc8aa43213c1487
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1 \
    HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 HF_HUB_DISABLE_TELEMETRY=1 \
    HF_HUB_DISABLE_IMPLICIT_TOKEN=1 HF_HUB_DISABLE_XET=1 \
    EMBEDDING_CACHE_DIR=/models
WORKDIR /app
COPY requirements.txt /app/requirements.txt
# Fully pinned closure; torch uses a hashed official CPU wheel, not a second index.
# No model is downloaded at build or startup.
RUN python -m pip install --no-cache-dir --no-deps --only-binary=:all: \
    --index-url https://pypi.org/simple -r /app/requirements.txt \
    && python -m pip check
COPY --chown=65532:65532 service.py /app/service.py
USER 65532:65532
EXPOSE 8080
# Mount a trusted pre-provisioned BAAI/bge-m3 safetensors cache read-only at /models.
# A read-only root filesystem also requires writable /tmp (e.g. a bounded tmpfs)
# for Torch import-time caches; keep /models read-only.
# Set required EMBEDDING_MODEL_REVISION (immutable 40-hex commit) and
# EMBEDDING_API_KEY via runtime configuration/secrets. The service must retain
# local_files_only=True, trust_remote_code=False and use_safetensors=True.
HEALTHCHECK --interval=30s --timeout=3s --start-period=120s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8080/health', timeout=2)"
CMD ["python", "/app/service.py"]
