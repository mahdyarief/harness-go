# HSG staging — deploy notes

Staging environment for staged user testing, one feature at a time.

| Hostname | Serves |
| --- | --- |
| `https://hsg.aspirecx.my.id` | SPA (embedded in the Go binary) |
| `https://api-hsg.aspirecx.my.id` | Same binary, API paths |

Both hostnames route through one Cloudflare Tunnel to the same container, so a
frontend change and an API change always ship together.

## Layout

```
docker-compose.yml     db (postgres:18) + api + embedding (bge-m3) + tunnel
hsg.env            runtime config and secrets (mode 600, never commit)
.env                   compose interpolation: DB/tunnel/embedding secrets
embedding/             self-hosted bge-m3 HTTP service (Dockerfile + service.py)
scripts/init-db.sh     one-time: migrations + runtime roles + embedding config
scripts/release-fast.sh host-built binaries -> runtime-only image -> recreate api
scripts/release.sh     reproducible from-source image build -> recreate api
scripts/verify-user.sh staging-only: mark an email verified without Resend
```

## First run (already done on this machine)

Environment lokal disalin dari template; jangan commit `.env` atau `hsg.env`.

```sh
cp .env.example .env
cp hsg.env.example hsg.env
# Isi domain dan secret yang sesuai sebelum menjalankan stack.
cd /home/dy/nusantech/hsg/deploy
docker compose up -d db
./scripts/init-db.sh
docker compose up -d api tunnel
```

`init-db.sh` must run before the API starts: the tenant connection pool refuses
to boot if `hsg_tenant` is missing or unsafe (owner, superuser, or
`BYPASSRLS`). It also creates the `hsg_embedding_worker` login, then runs
`migrate seed-embedding` to write the bge-m3 profile and a sealed credential
into `knowledge_embedding_config` (endpoint `http://embedding:8080/v1/embeddings`).

## Self-hosted embedding (bge-m3)

`embedding` runs the pinned BAAI/bge-m3 revision in-process on CPU and is only
reachable on the compose network; it is never published to the host. The model
weights are not baked into the image — the container mounts the
`embedding_cache` volume read-only and loads with `local_files_only=True`.

The volume must be provisioned once, before `docker compose up -d embedding`:

```sh
# Populate the HF cache with the exact revision in EMBEDDING_MODEL_REVISION.
docker run --rm --network host -e HF_HUB_OFFLINE=0 \
  -v hsg-deploy_embedding_cache:/models \
  --entrypoint /usr/local/bin/python hsg-embedding:local -c \
  "from huggingface_hub import snapshot_download; snapshot_download('BAAI/bge-m3', revision='<40-hex-revision>', cache_dir='/models')"
```

The image sets `HF_HUB_OFFLINE=1`, hence the explicit `HF_HUB_OFFLINE=0` for the
download. A fresh volume is root-owned while the service runs as uid 65532, so
`chown 65532:65532 /models` once before the download.

Config keys (`.env` and `hsg.env` must agree on `EMBEDDING_API_KEY`):

| Key | Purpose |
| --- | --- |
| `EMBEDDING_MODEL_REVISION` | 40-hex commit; must match the cached snapshot |
| `EMBEDDING_API_KEY` | bearer token the service and the worker both use |
| `EMBEDDING_MAX_CONCURRENCY` | 1..8; keep 1 on CPU — inference already uses all torch threads, so more concurrency only adds contention |
| `EMBEDDING_WORKER_DB_PASSWORD` | password for `hsg_embedding_worker` |

Sanity check after `docker compose up -d embedding`:

```sh
docker run --rm --network hsg-deploy_default curlimages/curl -s \
  -X POST http://embedding:8080/v1/embeddings \
  -H "Authorization: Bearer $EMBEDDING_API_KEY" \
  -H "X-Embedding-Revision: $EMBEDDING_MODEL_REVISION" \
  -H "Content-Type: application/json" \
  -d '{"input":["Halo dunia"],"model":"BAAI/bge-m3"}'
```

## Shipping a feature

```sh
./scripts/release-fast.sh
```

Builds the SPA and the Go binaries on the host, then assembles a runtime-only
image from `server/Dockerfile.runtime` and recreates only the `api` container.
The slow Docker stages (node install, Go module download, SPA build inside the
image) are skipped, which is what makes it faster on this machine. Database and
tunnel stay up, so testers keep their sessions.

Flags: `--with-migrations` runs `init-db.sh` first, `--no-health-wait` skips the
`/health` probe, `--build-only` stops after the image is built.

```sh
./scripts/release.sh
```

Still available as the reproducible from-source build: it compiles everything
inside Docker using `server/Dockerfile`. Use it from a clean machine or CI,
where the host may not have Go, Node, or `web/node_modules`.

If a migration was added, run it before the release:

```sh
./scripts/init-db.sh   # idempotent: `migrate up` plus a re-grant
```

## Onboarding a tester

1. Tester signs up at `https://hsg.aspirecx.my.id`.
2. Sign-in is refused until the address is verified, and the verification email
   currently fails — `RESEND_API_KEY` in `hsg.env` is a placeholder.
3. Until a real Resend key is set, verify manually:

```sh
./scripts/verify-user.sh tester@example.com
```

## Environment

`hsg.env` holds the runtime config. Two values change per environment:

- `CORS_ALLOWED_ORIGINS=https://hsg.aspirecx.my.id` — the browser origin.
  The API rejects requests from any other origin.
- `AUTH_BASE_URL=https://api-hsg.aspirecx.my.id` — what Authula puts in
  verification links.

`TRUSTED_PROXIES=172.16.0.0/12` lets the rate limiter see the real client IP
through the tunnel instead of the tunnel's container address.

The API fatals at boot when `FROM_ADDRESS` or `RESEND_API_KEY` is empty; Authula's
email plugin is always enabled. The staging values are therefore a sender
address on the zone plus a placeholder key.

### Rotating secrets

| Change | Effect |
| --- | --- |
| `APP_SECRET` | Invalidates every issued JWT; all testers must sign in again |
| `SUPER_USER` + `SEED_ON_BOOT=true` | Resets the superuser password on next boot |
| `LLM_ENCRYPTION_KEY` | Makes the stored LLM credential undecryptable; re-enter it |
| `POSTGRES_PASSWORD` | Also update `.env` and recreate `db`/`api` |
| `TENANT_DB_PASSWORD` | Also update `.env`, then re-run `init-db.sh` |

## Verification performed

- `/health` on both hostnames returns 200.
- Streaming chat turns complete: a provider-side SSE cut (e.g. `idrouter.us.ci`
  dropping the stream) is resumed by the runner from the last delivered text,
  bounded by `MaxPartialContinuations` (default 2), and the turn ends with a full
  answer rather than a partial/error message.
- A tenant user's chat cannot read or write another tenant's data even with the
  same browser session; the connection role is non-owner and RLS is forced.
- SPA deep link (`/sign-in`) and hashed assets return 200.
- The `services-*.js` chunk contains `api-hsg.aspirecx.my.id`, confirming the
  split-origin build.
- CORS preflight returns `access-control-allow-origin: https://hsg.aspirecx.my.id`.
- Superuser sign-in returns a token; a seeded tester account signs in after
  `verify-user.sh`.
- `/api/v1/platform/llm-config` returns 403 for a non-superuser.
- LLM config save rejects unknown fields (400), disallowed endpoint hosts (422),
  and keeps the credential write-only (`has_credential: true`, secret absent from
  the GET response). An audit row is written per save.
- `llm-test` returns 409 while the config is disabled.
- `hsg_tenant` sees zero tenant rows with no `app.tenant_id` set, and RLS
  with `FORCE ROW LEVEL SECURITY` is on for all three tenant tables.
- A tester sees an empty tenant list and gets 404 on another tenant's id.

## Known gaps

- **Resend**: no usable API key, so verification mail is not delivered.
- **Cloudflare token**: the API token used for setup was pasted in chat and
  should be rotated. It lives at `~/.cloudflared/hsg.token`.
- **Apex domain**: `aspirexc.my.id` itself returns NXDOMAIN with no apex record.
  The subdomains resolve and work; only the bare apex is unconfigured.