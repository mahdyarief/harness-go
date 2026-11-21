# HSG starter foundation

Ported from [govi-starter-kit](https://github.com/mahdyarief/govi-starter-kit)
at revision `9ec1944c9beddd4af9f33fb92a31622ddcaec52b`. Original copyright
and MIT licensing are retained in `LICENSE` and `server/LICENSE.md`.
This port establishes the starter foundation, tenant isolation and a limited
synthetic AI harness. It does not process claims or patient data.

A multi-tenant full-stack foundation: a Go API on Fiber v3 with PostgreSQL 18 and
[Authula](https://github.com/Authula/authula) embedded as a library, plus a
React 19 SPA on Vite and TanStack Router. The SPA is compiled into the API
binary, so production is one artifact with no separate asset server.

```
server/   Go API, migrations, seeders, Dockerfile
web/      React SPA (Vite, TanStack Router/Query/Form/Table, shadcn/ui)
```

## Requirements

- Go 1.27.3 or newer (the module declares `go 1.27.3`; `GOTOOLCHAIN=auto`
  fetches it if your local Go is older)
- Node 22 (see `.nvmrc`) and pnpm 11.10.0, pinned through
  `web/package.json#packageManager`. Run pnpm via `corepack pnpm` — pnpm is not
  assumed on your PATH.
- PostgreSQL 18 — the migrations use `uuidv7()`
- Docker, only if you want the bundled database

## Quick start

```sh
make install          # pnpm install + go mod download
make db-up            # PostgreSQL 18 in Docker on :5432
cp server/.env.example server/.env
cp web/.env.example web/.env
make dev              # API on :8080, web on :3000
```

Before `make dev`, set a unique `APP_SECRET` (at least 32 characters) in
`server/.env`. Siapkan pula role runtime tenant terpisah dan isi
`TENANT_DB_DSN`; role tersebut tidak boleh menjadi pemilik tabel, superuser,
memiliki `BYPASSRLS`, atau menjadi anggota role istimewa. Pool tenant tidak
boleh menggunakan credential migrasi atau Authula. Berikan hanya hak tabel
yang dibutuhkan setelah migrasi, bukan hak DDL. Startup menolak role tenant
yang tidak memenuhi pembatasan tersebut.

No default admin account is configured. To create one, set
`SUPER_USER=email:password` with a unique 12–128 character password and enable
`SEED_ON_BOOT=true` for one run. Then disable seeding; future seeded boots reset
that account's password to the configured value.

`make dev` runs both processes; Ctrl-C stops them together. Open
http://localhost:3000. The API applies migrations on boot because
`MIGRATE_ON_BOOT=true`.

The Compose database and example DSN use public **local-development-only**
credentials and bind PostgreSQL to loopback. Never use those credentials in
production; configure a separate database and secure credentials first.

## Layout

| Path | What lives there |
| --- | --- |
| `server/cmd/api` | HTTP entry point, routes, Authula wiring |
| `server/cmd/migrate` | Migration and seeder CLI |
| `server/internal` | Config, handlers, repositories, middlewares, models, DTOs |
| `server/migrations` | Versioned SQL migrations |
| `server/internal/web` | `go:embed` SPA bundle plus its fallback handler |
| `web/src/routes` | File-based TanStack Router routes |
| `web/src/components` | Layout, UI primitives, and feature blocks |
| `web/src/lib/api` | Models, DTOs, services, and TanStack Query options |

## API contract

Everything lives under `/api/v1`. Authula keeps its native response shapes
(no `{data}` envelope); the application's own endpoints answer with
`{data, metadata?, message?}`.

| Method | Path | Notes |
| --- | --- | --- |
| `POST` | `/api/v1/auth/email-password/sign-in` | Returns `{user, session, access_token, refresh_token, expires_at, expires_in}` |
| `POST` | `/api/v1/auth/token/refresh` | Returns `{access_token, refresh_token}` |
| `POST` | `/api/v1/auth/sign-out` | Ends the session |
| `GET` | `/api/v1/me` | `{data}` envelope; the app role lives here, not in Authula |
| `GET` | `/api/v1/categories` | Public list |
| `POST` `PUT` `DELETE` | `/api/v1/categories` | Super user only, idempotency guarded |

Roles are `super_user` and `user`, stored in the `users` mirror table. The
frontend reads them from `GET /api/v1/me`, because Authula does not know about
application roles.

## Commands

Root `Makefile`:

| Target | Does |
| --- | --- |
| `install` | Install web dependencies and download Go modules |
| `dev` | Run API and Vite dev server together |
| `dev-server` | Run only the API |
| `dev-web` | Run only the Vite dev server |
| `build` | Build the SPA and embed it into the API binary |
| `build-web` | Build the SPA only, into `web/dist` |
| `test` | Run the Go test suite |
| `lint` | `go vet` plus `tsc --noEmit` |
| `db-up`, `db-down`, `db-reset` | Manage the Dockerized PostgreSQL |
| `clean` | Remove build output |

`server/Makefile` keeps the migration targets (`db/migrations/up`,
`db/migrations/down`, `db/migrations/new name=...`) and the Go release builds.

## Fondasi SaaS dan AI

Platform admin (`super_user`) dapat membuat tenant dan mengelola konfigurasi LLM global. Role tersebut tidak otomatis memberikan akses ke tenant; akses tenant tetap memerlukan membership `tenant_admin` atau `reviewer`. Penetapan membership diperiksa server pada setiap transaksi. Pool tenant harus memakai role database terpisah, bukan pemilik tabel.

Konfigurasi LLM tersedia melalui `GET/PUT /api/v1/platform/llm-config`; `POST /api/v1/platform/llm-test` hanya menjalankan demo sintetis tetap. Isi endpoint chat completions lengkap, bukan URL dasar provider. Host endpoint harus tercantum persis dalam `LLM_ALLOWED_HOSTS`. Alamat privat, redirect, proxy environment, dan DNS yang mencampur alamat publik/privat ditolak.

Credential hanya dapat ditulis atau dihapus, tidak dibaca kembali melalui API. `LLM_ENCRYPTION_KEY` harus berisi base64 dari 32 byte acak dan disimpan di luar database; kehilangan kunci membutuhkan pengisian ulang credential. Mengubah endpoint membutuhkan credential baru atau penghapusan credential lama. LLM nonaktif secara default, tanpa fallback provider.

Harness belum memproses klaim atau data pasien. Demo memakai bukti fiktif server-side, satu tool read-only, dan validasi bukti/output. Kegagalan provider atau output tidak valid menghasilkan abstain, bukan keputusan klaim.

Batas output dikonfigurasi per panggilan (`max_tokens` pada `llm_config`) dengan nilai aktif **16.384** token (cek `1..16384`, default 16.384). Runner membatasi total token per run (`MaxTotalTokens`) hingga 32.768 agar multi-step tetap punya ruang, dengan pagar turn (default 6), tool-call (default 8), dan durasi (default 90 detik, maksimal 2 menit). Batas ini berlaku per run, bukan kuota biaya bulanan. Bila provider memutus stream SSE setelah mengeluarkan teks, runner melanjutkan jawaban dari titik terakhir (resume, maksimal 2 kali) sehingga turn berakhir lengkap, bukan parsial.

Sebelum deployment nyata, tetapkan persetujuan provider/region, kebijakan retensi dan pelatihan data, pengelolaan kunci, pembatasan egress jaringan, backup, serta audit operasional. Rate limiter dan idempotency starter masih in-memory; deployment multi-replica membutuhkan penyimpanan bersama.

## Basis Pengetahuan (Knowledge Base)

Fondasi menyertakan basis pengetahuan berbasis berkas yang memungkinkan model mencari dokumen internal dan mencantumkan kutipan. Lihat `server/README.md` untuk endpoint lengkap.

- **Unggah dokumen**: PDF, DOCX, XLSX, CSV, MD, TXT; maksimal 3 MiB per berkas. Super user mengelola pustaka global; pengguna tenant mengelola pustaka tenant-nya.
- **Ekstraksi & chunking**: worker latar memilah teks, memotong menjadi passage (default target 1 800 karakter, maksimum 3 200, overlap 200), dan menyimpan blob mentah terenkripsi.
- **Embedding mandiri**: BAAI/bge-m3 (dense 1024-dimensi, cosine, `normalization=l2`, `preprocessing=kb-chunk-v1`), terpaket di deploy (`deploy/embedding/`) dan disegel credential-nya saat `init-db.sh`.
- **Retrieval hibrida**: BM25 leksikal + pencarian vektor semantik, digabung dengan Reciprocal Rank Fusion (RRF) berbobot. Tanpa embedding aktif, jatuh ke leksikal saja. Tiap passage memiliki `locator` (mis. "halaman 3, paragraf 2") yang menjadi rujukan kutipan.
- **Tool `search_knowledge`**: dijalankan agent saat pertanyaan menyentuh dokumen yang diizinkan untuk sesi tersebut; passage, judul, dan locator yang dikembalikan diperlakukan sebagai data referensi yang tidak terpercaya.
- **Kutipan & pratinjau**: pesan asisten menyertakan `references[]` (`{title, locator, excerpt, score}`); locator dapat diklik untuk membuka pratinjau yang diarahkan ke passage yang dikutip.
- **Metrik penggunaan**: pelacakan seberapa sering tiap dokumen menyumbang hasil `search_knowledge` (leksikal vs semantik) untuk mengevaluasi efektivitas dokumen.
- **Bahasa LLM**: `llm-config` menerima `language` (default `"id"`) yang disuntikkan ke system prompt.

## Production build

```sh
make build
```

That runs the Vite build, copies `web/dist` into `server/internal/web/dist`,
and compiles `server/bin/api`. The binary serves the API and the SPA; unknown
`/api/*` paths still 404 instead of falling back to `index.html`.

For a container:

```sh
docker build -f server/Dockerfile -t hsg .
docker run --rm -p 8080:8080 --env-file server/.env hsg
```

The image is multi-stage (Node for the SPA, Go for the binary) and runs
distroless as a non-root user.

## Environment

`server/.env.example` documents every server setting; the important ones:

| Variable | Notes |
| --- | --- |
| `APP_SECRET` | At least 32 characters; signs Authula tokens |
| `DB_DSN` | PostgreSQL URL; `AUTHULA_DATABASE_URL` must not be set |
| `TENANT_DB_DSN` | Pool tenant terpisah, tanpa privilege owner atau bypass RLS |
| `LLM_ENCRYPTION_KEY` | Kunci credential platform, base64 dari 32 byte acak |
| `LLM_ALLOWED_HOSTS` | Host HTTPS persis dipisahkan koma, tanpa port atau wildcard |
| `EMBEDDING_WORKER_DB_DSN` | Opsional; kosong menonaktifkan worker. Gunakan role LOGIN terisolasi dengan membership `hsg_embedding_worker`, tanpa akses tabel langsung |
| `EMBEDDING_ALLOWED_HOSTS` | Izin host embedding persis, terpisah dari LLM; kosong menolak semua host |
| `EMBEDDING_ALLOW_PRIVATE` | Default `false`; hanya untuk endpoint self-hosted pada jaringan privat tepercaya |
| `ENV` | `development`, `staging`, or `production`; unset means production |
| `MIGRATE_ON_BOOT` | Apply embedded migrations at startup |
| `SEED_ON_BOOT` | Run idempotent seeders at startup (needs `SUPER_USER`) |
| `CORS_ALLOWED_ORIGINS` | Comma-separated browser origins |

`web/.env.example` has `VITE_APP_NAME` and `VITE_API_URL`. Leave
`VITE_API_URL` empty to use the same origin; the dev server proxies `/api` to
`VITE_API_PROXY_TARGET` (default `http://localhost:8080`).
