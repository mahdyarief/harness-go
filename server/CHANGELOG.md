# Changelog

All notable changes to this project are documented in this file. Versions follow the git tags (`v*`); Docker images are published to ghcr.io with the same version.

## [Unreleased]

### Fondasi SaaS dan AI

- Fondasi HSG: multi-tenant SaaS, konfigurasi LLM global, dan harness chat agent (`d569c18`).
- Alokasi token per-tenant dan debug log tool Agent (`87f3322`).
- Uji koneksi LLM dipisahkan dari demo; penolakan akses chat diperjelas (`1698e50`).
- Chat pribadi platform untuk `super_user`, ledger pemakaian append-only, dan batas output 16.384 token (`c361ec6`).
- Audit log dan audit trail append-only (`10ecf4f`).
- Konfigurasi deploy Docker staging dan atribusi Beautiful UI (`cc664de`).

### Harness agent

- Batas hasil tool, timeout, dan sinyal konteks (`d37aadf`).
- Kompaksi dua tingkat dan metrik run (`bbb245e`).
- Dorong model melanjutkan setelah memakai tool (`904cc60`).
- Guard harness dengan guard error berulang dan kerangka guard domain (`4a46f17`).
- Resume stream terputus: runner melanjutkan dari teks terakhir (maksimal 2 kali) sehingga turn berakhir lengkap, bukan parsial (`dc23f8c`); jawaban parsial tetap disimpan bila batas habis (`b105a56`).
- `WriteTimeout` dinaikkan agar turn streaming tidak terpotong (`fb61d94`).

### Basis pengetahuan dan RAG

- Basis pengetahuan berbasis berkas dengan retrieval chat terisolasi (`cc01a08`).
- Metadata hasil ekstraksi dan pratinjau teks knowledge (`2928a4d`).
- Retrieval hibrida: BM25 leksikal + pgvector semantik dengan weighted RRF (`148ca44`).
- Dukungan PDF dan embedding mandiri BGE-M3 terpaket di deploy (`4fb2856`).
- Metrik penggunaan embedding dan dokumen (`1271d6d`).
- Referensi dokumen chat dan pengaturan bahasa LLM (`db954a1`); kolom `references` dikutip agar tidak bentrok dengan kata cadangan Postgres (`beeb404`).
- Kutipan KB dan highlight pratinjau referensi (`7a9c439`, `2897e60`); null references dan toggle search knowledge diperbaiki (`751d896`).
- Polling embedding worker tidak lagi mencatat idle sebagai kegagalan claim (`b59828d`).

### Chat dan antarmuka

- Integrasi Exa web access dan tab Konfigurasi AI (`b655174`); pencarian web eksplisit dan render markdown (`3f29924`).
- Toggle KB dan web dipindah ke baris bawah prompt (`c45b2a1`, `d0e41c0`).
- Transkrip chat berpaginasi dan panel percakapan tinggi tetap (`9117a9b`).
- Loading state chat: skeleton, indikator pesan lama, blur tepi atas (`6c687c9`).
- Tenant switcher dan daftar tenant collapsed (`84e49c5`).
- Theme toggler animasi, label tunggu mengetik, daftar sesi collapsible, drawer log tool (`97e214e`).
- Redesign chat dan migrasi UI ke token Beautiful UI (`6abf530`).
- Semua kontrol UI disamakan dengan design system (`c26bf01`).

### Deploy dan operasional

- Jalur deploy cepat dengan binary hasil build host (`9785219`).
- Deploy Docker staging dengan Cloudflare tunnel (`cc664de`).
