# Pre-commit Quality Gate Design

**Goal:** Jaga kerapian dan standarisasi kode otomatis saat `git commit`, tanpa memaksa semua contributor menginstal tool berat, dan dengan aturan yang adil untuk utang teknis yang sudah ada.

**Status:** disetujui 2026-11-21.

## Konteks dan masalah

Repo `harness-go` adalah monorepo dua bagian: API Go di `server/` dan SPA React/TypeScript di `web/`. `Makefile` root sudah punya `check`, `lint`, `fmt`, dan `audit`. Namun tidak ada gate yang jalan otomatis di `git commit`, sehingga pelanggaran formatting, file yang membengkak, dan commit message yang tidak konsisten baru ketahuan di CI atau saat review.

Fakta yang memengaruhi desain:

- `corepack` tidak tersedia di environmen saat ini, tetapi `pnpm` 10.28.2 ada di PATH. `web/package.json` memakai `pnpm@11.10.0`.
- `golangci-lint`, `gitleaks`, dan `detect-secrets` tidak terpasang. Gate tidak boleh bergantung pada tool yang tidak ada.
- Ada file lama dan vendored di atas 500 baris, antara lain `server/internal/handlers/chat.go` (847), `web/src/components/ui/sidebar.tsx` (665), `web/src/components/block/knowledge/table.tsx` (595), dan `web/src/routeTree.gen.ts` (generated). Aturan 500 baris tidak boleh langsung menggagalkan commit yang hanya menyentuh file tersebut secara wajar.
- `CONTRIBUTING.md` sudah menetapkan Conventional Commits, `gofmt`, Prettier, ESLint, dan `tsc --noEmit`. Gate harus memperkuat aturan yang sudah ada, bukan menggantinya.

## Arsitektur

### Komponen

1. `scripts/pre_commit_check.py`
   - Satu script Python mandiri, tanpa dependency pihak ketiga.
   - Mode: `files` (default, dipakai hook `pre-commit`) dan `message` (dipakai hook `commit-msg`).
   - Dalam mode `files`, script mengambil daftar file yang di-stage dengan `git diff --cached --name-only --diff-filter=ACM`.
   - Dalam mode `message`, script membaca pesan commit dari argumen path (dikirim Husky sebagai `$1`).
   - Satu-satunya source of truth untuk semua aturan, supaya hook lokal dan `make check-commit` identik.

2. Hook Husky
   - `.husky/pre-commit`: menjalankan `python3 scripts/pre_commit_check.py files`.
   - `.husky/commit-msg`: menjalankan `python3 scripts/pre_commit_check.py message "$1"`.
   - Hook dibuat tipis: tidak ada logika bisnis di dalam hook.
   - Pemasangan via `prepare` script di `web/package.json` (`husky`), sehingga `pnpm install` otomatis mengaktifkan hook. Karena repo ini tidak punya `package.json` root dan `web/` sudah punya `package.json`, konfigurasi Husky ditempatkan di `web/`, tetapi hook ditargetkan ke root repo (`cd ..` sebelum memanggil checker). Tidak ada `lint-staged`: pemanggilan formatter/linter tetap dikendalikan checker Python supaya satu source of truth.

3. Konfigurasi aturan
   - `scripts/pre_commit_rules.json`: allowlist LOC, batas ukuran file, pola secret, dan daftar tipe commit yang diperbolehkan.
   - File ini dibaca script dan bisa diubah tanpa menyunting kode Python.

4. Target Makefile
   - `make check-commit`: menjalankan checker yang sama terhadap file yang saat ini di-stage dan pesan commit yang diberikan lewat `MSG=...`. Dipakai untuk verifikasi manual dan bisa dipanggil CI.
   - `make install` tetap menjadi entry point; dokumentasi diperbarui untuk menjelaskan hook otomatis.

### Alur data

```
git commit
  ├─ Husky pre-commit ──> scripts/pre_commit_check.py files
  │                        ├─ git diff --cached --name-only
  │                        ├─ LOC check (dengan allowlist)
  │                        ├─ gofmt -l pada .go yang di-stage
  │                        ├─ prettier --check pada file web yang di-stage (jika tersedia)
  │                        ├─ go vet pada paket Go yang tersentuh
  │                        ├─ eslint pada file web yang di-stage (jika tersedia)
  │                        └─ secret & hygiene scan
  └─ Husky commit-msg ──> scripts/pre_commit_check.py message "$1"
                           └─ Conventional Commits validation
```

Jika `node_modules/husky` belum ada, hook tidak terpasang dan tidak ada yang jalan — contributor tetap bisa commit. Setelah `pnpm install`, hook aktif. Jika `web/node_modules` belum ada tapi hook sudah aktif, checker tetap menjalankan bagian Python (LOC, secret, commit message, gofmt) dan mencetak peringatan untuk Prettier/ESLint, bukan error.

## Aturan detail

### 1. Batas 500 LOC

- Berlaku untuk `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.json`, `.sql`, dan `.sh` yang di-stage sebagai added atau modified.
- Dihitung dari file hasil kerja (working tree), bukan dari blob lama.
- File **ditolak** hanya jika:
  - file baru memiliki lebih dari 500 baris, atau
  - file yang sudah ada sebelumnya ≤500 baris menjadi >500 baris, atau
  - file yang sudah ada >500 baris bertambah panjang melewati batas yang sudah ada (misal 847 → 860).
- File yang sudah >500 baris dan **tidak bertambah** (misal tetap 847 atau justru menyusut) tidak ditolak.
- Allowlist (tetap boleh ditambah/diubah, tapi tidak boleh melewati batas + pengecualian harus alasan jelas):
  - `web/src/components/ui/**` — vendored shadcn/ui, upstream code sengaja dijaga utuh.
  - `web/src/routeTree.gen.ts` — generated oleh TanStack Router.
  - `server/migrations/**/*.sql` — migration sering panjang dan berurutan.
  - `**/*_test.go` dan `server/cmd/api/*_integration_test.go` — tabel data uji panjang adalah pola normal.
- Pesan error memuat jumlah baris sekarang, batas, dan saran memecah file.

### 2. Format dan lint

- Go: `gofmt -l <file>` harus kosong. `go vet` dijalankan pada package unik dari file Go yang di-stage.
- Web: `prettier --check <file>` dan `eslint <file>` dijalankan dari `web/`, hanya jika `web/node_modules/.bin/prettier` dan `web/node_modules/.bin/eslint` tersedia. Jika tidak ada, dicetak `WARN: skipped web lint, node_modules missing`.
- `tsc --noEmit` sengaja tidak dimasukkan ke pre-commit karena lambat dan sudah ada di `make check`; gate ini fokus pada file yang berubah.

### 3. Conventional Commits

- Format: `type(scope): subject` atau `type: subject`.
- Type diperbolehkan: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `ci`, `build`, `revert`.
- Subject minimal 3 karakter, tidak diakhiri titik.
- Merge commit, revert commit bawaan Git, dan pesan yang diawali `fixup!`/`squash!` dilewati.
- Pesan error mencontohkan format yang benar.

### 4. Secret dan hygiene

Pola yang ditolak (dicek pada baris yang ditambahkan saja):

- RSA/OPENSSH/EC/DSA private key header.
- AWS access key (`AKIA...`, `ASIA...`).
- GitHub token (`ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`).
- Slack token (`xoxb-`, `xoxp-`, `xoxa-`).
- Penugasan literal `password=`, `api_key=`, `secret=`, `token=` dengan nilai bukan placeholder jelas (`changeme`, `example`, `xxx`, `...`, variabel `$`).
- Marker konflik Git (`<<<<<<<`, `=======`, `>>>>>>>`) pada baris baru.
- Trailing whitespace pada baris baru.
- File `.env` yang ikut ke-stage (kecuali `*.example`).
- File baru/besaran >1 MB.

Karena checker hanya melihat baris yang ditambahkan (`git diff --cached --unified=0`), file lama yang sudah mengandung pola tidak langsung menggagal commit berikutnya.

### 5. Keluaran dan exit code

- Satu baris per pelanggaran: `[FAIL] <kategori> <file>:<baris> — <pesan>`.
- Ringkasan di akhir: `<N> error(s), <M> warning(s)`.
- Exit `1` jika ada error, `0` jika hanya warning atau bersih.
- `--help` menampilkan pemakaian.

## Testing

Script Python diuji dengan `unittest` di `scripts/test_pre_commit_check.py`, dengan fixtures sementara untuk:

- LOC: file baru >500, file lama ≤500 yang membengkak, file lama >500 yang tidak bertambah, file dalam allowlist, dan file yang menyusut.
- Commit message: format valid, type tidak dikenal, subject terlalu pendek, merge commit dilewati.
- Secret: private key, AWS key, GitHub token, placeholder yang aman, trailing whitespace, konflik marker.
- Integrasi ringan: menjalankan checker pada repo nyata dengan file yang di-stage dan memastikan exit code sesuai.

Verifikasi manual:

```sh
cd harness-go
python3 scripts/test_pre_commit_check.py
make check-commit MSG="feat: contoh pesan"
# Simulasikan hook:
git add <file> && python3 scripts/pre_commit_check.py files
```

## Dampak dan risiko

- Hook Husky hanya aktif setelah `pnpm install`. Ini disengaja supaya clone baru tidak langsung mengubah perilaku Git sebelum dependency ada.
- `pnpm` di environment ini versi 10.28.2, sementara `packageManager` di `web/package.json` 11.10.0. `husky` akan dipasang lewat `prepare` saat install normal; tidak ada perubahan pada versi package.
- Checker Python memanggil `git`, `gofmt`, `prettier`, dan `eslint` lewat `subprocess` dengan `cwd` repo root / `web`. Tidak ada `shell=True`, sehingga aman dari command injection pada nama file.
- Allowlist adalah celah yang disengaja. Setiap penambahan allowlist baru harus disertai alasan di `pre_commit_rules.json`.

## Out of scope

- Mengubah aturan gofi/vine layer audit (`make audit`) — sudah ada dan tidak disentuh.
- Menjalankan test suite lengkap di hook — terlalu lambat, tetap di `make check` / CI.
- Memaksa install `golangci-lint`, `gitleaks`, atau `detect-secrets`.
- Auto-fix formatting di hook. Formatter tetap dijalankan manual lewat `make fmt`.
