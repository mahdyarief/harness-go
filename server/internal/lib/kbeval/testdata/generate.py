"""Regenerate public synthetic fixtures; no provider, database or patient data."""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent
fixture = {"scope": "global", "id": "synthetic-kb-v1", "provenance": "machine-generated synthetic templates; NOT human ground truth; fictional equipment rules, no patient data; generator generate.py v1", "chunks": [], "queries": []}
for n in range(1, 21):
    code, limit = f"VX-{n:03}", 20 + n
    a, b, c = f"c{n:03}a", f"c{n:03}b", f"c{n:03}c"
    fixture["chunks"] += [
        {"id": a, "document_id": f"manual-{n:03}", "text": f"Manual {code}: periksa filter setiap {limit} hari. Hentikan perangkat saat indikator merah."},
        {"id": b, "document_id": f"manual-{n:03}", "text": f"Tabel {code}: mode | daya watt; siaga | {n}; aktif | {n * 10}."},
        {"id": c, "document_id": f"bulletin-{n:03}", "text": f"Buletin {code}: setelah indikator merah, teknisi harus mereset sensor sebelum restart."}]
    prompts = [
        ("exact_term", f"Apa interval pemeriksaan filter kode {code}?", [(a, 3)]),
        ("paraphrase_id", f"Untuk unit {code}, berapa lama jeda mengecek penyaring?", [(a, 3)]),
        ("mixed_language", f"How to stop {code} saat red indicator menyala?", [(a, 3), (c, 1)]),
        ("table", f"Berapa watt mode aktif di tabel {code}?", [(b, 3)]),
        ("multi_source", f"Gabungkan manual dan buletin {code}: tindakan indikator merah sampai restart?", [(a, 3), (c, 3)]),
        ("ambiguous", f"Bagaimana aturan operasi {code}?", [(a, 2), (b, 1), (c, 2)])]
    for category, text, labels in prompts:
        fixture["queries"].append({"id": f"q{n:03}-{category}", "split": "dev" if n <= 14 else "heldout", "category": category, "text": text, "no_answer": False, "labels": [{"chunk_id": cid, "grade": grade} for cid, grade in labels]})
for n in range(1, 31):
    fixture["queries"].append({"id": f"na{n:03}", "split": "dev" if n <= 21 else "heldout", "category": "no_answer", "text": f"Berapa harga kontrak garansi perangkat ZQ-{n:03} di wilayah fiksi {n}?", "no_answer": True, "labels": []})

def save(name, data):
    (ROOT / name).write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")

for chunk in fixture["chunks"]:
    chunk["scope"] = "global"
save("synthetic-v1.json", fixture)
for split in ("dev", "heldout"):
    run = {"fixture_id": fixture["id"], "run_id": f"illustrative-{split}", "split": split, "provenance": "machine-generated label-derived illustrative rankings; NOT retrieval output, NOT SQL baseline, NOT benchmark", "results": []}
    for q in fixture["queries"]:
        if q["split"] != split:
            continue
        ids = [label["chunk_id"] for label in q["labels"]]
        ids = ["c001b"] + [cid for cid in ids if cid != "c001b"]
        run["results"].append({"query_id": q["id"], "answered": not q["no_answer"], "candidates": [{"chunk_id": cid, "rank": i + 1} for i, cid in enumerate(ids)]})
    save(f"illustrative-{split}.json", run)
