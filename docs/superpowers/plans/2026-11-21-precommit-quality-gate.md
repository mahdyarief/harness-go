# Pre-commit Quality Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Pasang gate otomatis di `git commit` yang mengecek LOC, format, lint, pesan commit, dan secret/hygiene hanya pada file yang di-stage.

**Architecture:** Satu script Python mandiri (`scripts/pre_commit_check.py`) menjadi source of truth untuk semua aturan. Hook Husky tipis memanggilnya dalam mode `files` dan `message`. Aturan yang bisa diubah ditempatkan di JSON. Target `make check-commit` menjalankan checker yang sama supaya perilaku lokal dan CI identik.

**Tech Stack:** Python 3 stdlib (`unittest`, `subprocess`, `fnmatch`, `re`, `json`), Husky 9 via `web/package.json`, `gofmt`, `go vet`, Prettier, ESLint.

## Global Constraints

- `loc_limit`: 500.
- File baru atau yang membesar melewati 500 ditolak; file yang sudah >500 dan tidak bertambah diperbolehkan.
- Allowlist LOC: `web/src/components/ui/**`, `web/src/routeTree.gen.ts`, `server/migrations/**/*.sql`, `**/*_test.go`, `server/cmd/api/*_integration_test.go`, `scripts/test_*.py`.
- `max_file_bytes`: 1048576.
- `commit_types`: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `ci`, `build`, `revert`.
- Conventional Commits: `type(scope): subject` atau `type: subject`, subject minimal 3 karakter, tidak diakhiri titik.
- Web lint dilewati dengan warning jika `web/node_modules` tidak ada.
- Tidak ada `shell=True` di `subprocess`.
- Satu source of truth: tidak ada `lint-staged`.
- Semua path relatif terhadap root repo.

---

### Task 1: Fondasi checker dan rules JSON

**Files:**
- Create: `scripts/pre_commit_rules.json`
- Create: `scripts/pre_commit_check.py`
- Test: `scripts/test_pre_commit_check.py`

**Interfaces:**
- Produces: `Issue` dataclass dengan `.render()`, `load_rules(path=None) -> dict`, `REPO_ROOT`, `RULES_PATH`.

- [x] **Step 1: Tulis test yang gagal untuk fondasi**

Isi `scripts/test_pre_commit_check.py`:
```python
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import pre_commit_check as checker


class FoundationTest(unittest.TestCase):
    def test_load_rules_reads_json(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "rules.json"
            path.write_text(json.dumps({"loc_limit": 500}), encoding="utf-8")
            self.assertEqual(checker.load_rules(path)["loc_limit"], 500)

    def test_issue_render_includes_location(self):
        issue = checker.Issue("loc", "server/a.go", 12, "too long")
        self.assertEqual(issue.render(), "[FAIL] loc server/a.go:12 — too long")


if __name__ == "__main__":
    unittest.main()
```

- [x] **Step 2: Jalankan test, verifikasi gagal**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: FAIL dengan `ModuleNotFoundError: No module named 'pre_commit_check'`.

- [x] **Step 3: Buat rules JSON**

Isi `scripts/pre_commit_rules.json`:
```json
{
  "loc_limit": 500,
  "loc_allowlist": [
    "web/src/components/ui/**",
    "web/src/routeTree.gen.ts",
    "server/migrations/**/*.sql",
    "**/*_test.go",
    "server/cmd/api/*_integration_test.go",
    "scripts/test_*.py"
  ],
  "max_file_bytes": 1048576,
  "commit_types": ["feat", "fix", "docs", "chore", "refactor", "test", "perf", "ci", "build", "revert"],
  "secret_patterns": [
    {"name": "private_key", "regex": "-----BEGIN (RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----"},
    {"name": "aws_access_key", "regex": "\\b(?:AKIA|ASIA)[A-Z0-9]{16}\\b"},
    {"name": "github_token", "regex": "\\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\\b"},
    {"name": "slack_token", "regex": "\\bxox[baprs]-[A-Za-z0-9-]{10,}\\b"},
    {"name": "hardcoded_secret", "regex": "(?i)\\b(password|api_key|secret|token)\\s*[:=]\\s*['\\\"]([^'\\\"]{6,})['\\\"]"}
  ],
  "secret_placeholders": ["changeme", "example", "xxx", "...", "placeholder", "dummy"]
}
```

- [x] **Step 4: Implementasi fondasi checker**

Isi `scripts/pre_commit_check.py`:
```python
#!/usr/bin/env python3
"""Pre-commit quality gate for harness-go."""

from __future__ import annotations

import json
import re
import subprocess
import sys
from dataclasses import dataclass
from fnmatch import fnmatch
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
RULES_PATH = Path(__file__).resolve().parent / "pre_commit_rules.json"


@dataclass(frozen=True)
class Issue:
    category: str
    path: str
    line: int
    message: str

    def render(self) -> str:
        if self.line > 0:
            return f"[FAIL] {self.category} {self.path}:{self.line} — {self.message}"
        return f"[FAIL] {self.category} {self.path} — {self.message}"


@dataclass(frozen=True)
class Warning:
    message: str

    def render(self) -> str:
        return f"[WARN] {self.message}"


def load_rules(path: Path | None = None) -> dict:
    target = path or RULES_PATH
    return json.loads(target.read_text(encoding="utf-8"))


def git(*args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=REPO_ROOT, check=True, capture_output=True, text=True
    )
    return result.stdout


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if not args or args[0] in {"-h", "--help"}:
        print("usage: pre_commit_check.py files|message [<commit-msg-file>]")
        return 0
    print("checker mode not implemented yet")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

- [x] **Step 5: Jalankan test, verifikasi hijau**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: `OK` (2 tests).

- [x] **Step 6: Commit**

```bash
git add scripts/pre_commit_rules.json scripts/pre_commit_check.py scripts/test_pre_commit_check.py
git commit -m "feat: add pre-commit checker foundation"
```

---

### Task 2: Aturan LOC 500 dengan allowlist

**Files:**
- Modify: `scripts/pre_commit_check.py`
- Modify: `scripts/test_pre_commit_check.py`

**Interfaces:**
- Consumes: `Issue`, `load_rules`, `REPO_ROOT` dari Task 1.
- Produces: `check_loc(paths: list[str], rules: dict, repo_root: Path = REPO_ROOT) -> list[Issue]`.

- [x] **Step 1: Tulis test gagal untuk LOC**

Tambahkan ke `scripts/test_pre_commit_check.py`:
```python
class LocTest(unittest.TestCase):
    def setUp(self):
        self.rules = {
            "loc_limit": 5,
            "loc_allowlist": ["web/src/components/ui/**", "**/*_test.go"],
        }

    def make_repo(self):
        root = Path(tempfile.mkdtemp())
        (root / "server").mkdir()
        (root / "web/src/components/ui").mkdir(parents=True)
        return root

    def test_new_file_over_limit_fails(self):
        root = self.make_repo()
        target = root / "server/new.go"
        target.write_text("a\n" * 6, encoding="utf-8")
        issues = checker.check_loc(["server/new.go"], self.rules, root)
        self.assertEqual(len(issues), 1)
        self.assertEqual(issues[0].category, "loc")
        self.assertIn("6 lines", issues[0].message)

    def test_existing_file_growing_past_limit_fails(self):
        root = self.make_repo()
        target = root / "server/grow.go"
        target.write_text("a\n" * 6, encoding="utf-8")
        old = subprocess.run(
            ["git", "hash-object", "-w", str(target)], cwd=root,
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        target.write_text("a\n" * 8, encoding="utf-8")
        with unittest.mock.patch.object(checker, "git", return_value=old + "\n"):
            issues = checker.check_loc(["server/grow.go"], self.rules, root)
        self.assertEqual(len(issues), 1)

    def test_large_existing_file_not_growing_passes(self):
        root = self.make_repo()
        target = root / "server/old.go"
        target.write_text("a\n" * 800, encoding="utf-8")
        old = subprocess.run(
            ["git", "hash-object", "-w", str(target)], cwd=root,
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        with unittest.mock.patch.object(checker, "git", return_value=old + "\n"):
            issues = checker.check_loc(["server/old.go"], self.rules, root)
        self.assertEqual(issues, [])

    def test_allowlisted_file_passes(self):
        root = self.make_repo()
        target = root / "web/src/components/ui/sidebar.tsx"
        target.write_text("a\n" * 800, encoding="utf-8")
        issues = checker.check_loc(
            ["web/src/components/ui/sidebar.tsx"], self.rules, root
        )
        self.assertEqual(issues, [])
```
Tambahkan `import subprocess` dan `import unittest.mock` di bagian import test jika belum ada.

- [x] **Step 2: Jalankan test, verifikasi gagal**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: FAIL dengan `AttributeError: module 'pre_commit_check' has no attribute 'check_loc'`.

- [x] **Step 3: Implementasi `check_loc`**

Tambahkan ke `scripts/pre_commit_check.py`:
```python
LOC_EXTENSIONS = {".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".sql", ".sh"}


def allowed_by_allowlist(path: str, rules: dict) -> bool:
    normalized = path.replace("\\", "/")
    return any(fnmatch(normalized, pattern) for pattern in rules.get("loc_allowlist", []))


def line_count(path: Path) -> int:
    return len(path.read_text(encoding="utf-8", errors="replace").splitlines())


def old_line_count(path: str, repo_root: Path) -> int | None:
    try:
        blob = git("show", f":{path}")
    except subprocess.CalledProcessError:
        return None
    return len(blob.splitlines())


def check_loc(paths: list[str], rules: dict, repo_root: Path = REPO_ROOT) -> list[Issue]:
    limit = rules["loc_limit"]
    issues: list[Issue] = []
    for path in paths:
        suffix = Path(path).suffix.lower()
        if suffix not in LOC_EXTENSIONS or allowed_by_allowlist(path, rules):
            continue
        target = repo_root / path
        if not target.exists():
            continue
        current = line_count(target)
        if current <= limit:
            continue
        previous = old_line_count(path, repo_root)
        if previous is not None and current <= previous:
            continue
        issues.append(
            Issue(
                "loc",
                path,
                1,
                f"{current} lines exceeds {limit}; split the file or extend the documented allowlist",
            )
        )
    return issues
```

- [x] **Step 4: Jalankan test, verifikasi hijau**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: `OK` (6 tests). Jika `git show` gagal karena bukan repo, test `test_new_file_over_limit_fails` akan tetap hijau karena `previous is None`.

- [x] **Step 5: Commit**

```bash
git add scripts/pre_commit_check.py scripts/test_pre_commit_check.py
git commit -m "feat: enforce 500-line limit on staged files"
```

---

### Task 3: Validasi pesan commit Conventional Commits

**Files:**
- Modify: `scripts/pre_commit_check.py`
- Modify: `scripts/test_pre_commit_check.py`

**Interfaces:**
- Consumes: `Issue` dari Task 1, `rules["commit_types"]` dari `pre_commit_rules.json`.
- Produces: `check_message(path: Path, rules: dict) -> list[Issue]`.

- [x] **Step 1: Tulis test gagal untuk commit message**

Tambahkan ke `scripts/test_pre_commit_check.py`:
```python
class MessageTest(unittest.TestCase):
    rules = {"commit_types": ["feat", "fix", "docs", "chore"]}

    def check(self, message: str):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "COMMIT_EDITMSG"
            path.write_text(message, encoding="utf-8")
            return checker.check_message(path, self.rules)

    def test_valid_type_scope_subject(self):
        self.assertEqual(self.check("feat(harness): add resolver\n"), [])

    def test_valid_type_only(self):
        self.assertEqual(self.check("fix: correct truncation\n"), [])

    def test_unknown_type_fails(self):
        issues = self.check("wip: something\n")
        self.assertEqual(len(issues), 1)
        self.assertEqual(issues[0].category, "commit_message")

    def test_short_subject_fails(self):
        self.assertEqual(len(self.check("feat: ab\n")), 1)

    def test_trailing_period_fails(self):
        self.assertEqual(len(self.check("feat: add thing.\n")), 1)

    def test_merge_commit_is_skipped(self):
        self.assertEqual(self.check("Merge branch 'main' into x\n"), [])

    def test_fixup_is_skipped(self):
        self.assertEqual(self.check("fixup! feat: add thing\n"), [])
```

- [x] **Step 2: Jalankan test, verifikasi gagal**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: FAIL dengan `AttributeError: module 'pre_commit_check' has no attribute 'check_message'`.

- [x] **Step 3: Implementasi `check_message`**

Tambahkan ke `scripts/pre_commit_check.py`:
```python
COMMIT_RE = re.compile(r"^(?P<type>[a-z]+)(?:\((?P<scope>[^()]+)\))?: (?P<subject>.+)$")


def check_message(path: Path, rules: dict) -> list[Issue]:
    message = path.read_text(encoding="utf-8").strip()
    first = message.splitlines()[0] if message else ""
    if first.startswith(("Merge ", "Revert ", "fixup! ", "squash! ")):
        return []
    match = COMMIT_RE.match(first)
    if not match or match.group("type") not in rules["commit_types"]:
        return [
            Issue(
                "commit_message",
                "<message>",
                1,
                f"expected 'type(scope): subject'; types: {', '.join(rules['commit_types'])}",
            )
        ]
    subject = match.group("subject").strip()
    if len(subject) < 3 or subject.endswith("."):
        return [
            Issue(
                "commit_message",
                "<message>",
                1,
                "subject must be at least 3 characters and must not end with a period",
            )
        ]
    return []
```

- [x] **Step 4: Jalankan test, verifikasi hijau**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: `OK` (13 tests).

- [x] **Step 5: Commit**

```bash
git add scripts/pre_commit_check.py scripts/test_pre_commit_check.py
git commit -m "feat: validate conventional commit messages"
```

---

### Task 4: Secret dan hygiene scan

**Files:**
- Modify: `scripts/pre_commit_check.py`
- Modify: `scripts/test_pre_commit_check.py`

**Interfaces:**
- Consumes: `Issue`, `rules["secret_patterns"]`, `rules["secret_placeholders"]`, `rules["max_file_bytes"]`.
- Produces: `check_secret_and_hygiene(paths: list[str], rules: dict, repo_root: Path = REPO_ROOT) -> list[Issue]`.

- [x] **Step 1: Tulis test gagal untuk secret/hygiene**

Tambahkan ke `scripts/test_pre_commit_check.py`:
```python
class SecretTest(unittest.TestCase):
    def setUp(self):
        self.rules = {
            "max_file_bytes": 1024,
            "secret_patterns": [
                {"name": "aws_access_key", "regex": "\\bAKIA[A-Z0-9]{16}\\b"},
                {"name": "hardcoded_secret", "regex": "(?i)(password)\\s*[:=]\\s*['\\\"]([^'\\\"]{6,})['\\\"]"},
            ],
            "secret_placeholders": ["changeme", "example", "xxx"],
        }

    def scan(self, content: str, path: str = "server/config.go"):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            target = root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content, encoding="utf-8")
            return checker.check_secret_and_hygiene([path], self.rules, root)

    def test_aws_key_detected(self):
        issues = self.scan("key = 'AKIAIOSFODNN7EXAMPLE'\n")
        self.assertTrue(any(i.category == "secret" for i in issues))

    def test_hardcoded_password_detected(self):
        issues = self.scan("password = \"supersecret\"\n")
        self.assertTrue(any(i.category == "secret" for i in issues))

    def test_placeholder_password_allowed(self):
        self.assertEqual(self.scan("password = \"changeme\"\n"), [])

    def test_trailing_whitespace_detected(self):
        issues = self.scan("value = 1   \n")
        self.assertTrue(any(i.category == "hygiene" for i in issues))

    def test_conflict_marker_detected(self):
        issues = self.scan("<<<<<<< HEAD\n")
        self.assertTrue(any(i.category == "hygiene" for i in issues))

    def test_env_file_rejected(self):
        issues = self.scan("KEY=value\n", path=".env")
        self.assertTrue(any("env" in i.message.lower() for i in issues))

    def test_oversized_file_rejected(self):
        issues = self.scan("x" * 2000 + "\n")
        self.assertTrue(any("byte" in i.message.lower() for i in issues))
```

- [x] **Step 2: Jalankan test, verifikasi gagal**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: FAIL dengan `AttributeError: module 'pre_commit_check' has no attribute 'check_secret_and_hygiene'`.

- [x] **Step 3: Implementasi `check_secret_and_hygiene`**

Tambahkan ke `scripts/pre_commit_check.py`:
```python
CONFLICT_MARKERS = ("<<<<<<<", "=======", ">>>>>>>")


def added_lines(diff: str) -> list[tuple[int, str]]:
    lines: list[tuple[int, str]] = []
    new_line = 0
    for line in diff.splitlines():
        if line.startswith("@@"):
            match = re.search(r"\+(\d+)", line)
            new_line = int(match.group(1)) if match else 0
        elif line.startswith("+") and not line.startswith("+++"):
            lines.append((new_line, line[1:]))
            new_line += 1
        elif not line.startswith("-"):
            new_line += 1
    return lines


def check_secret_and_hygiene(
    paths: list[str], rules: dict, repo_root: Path = REPO_ROOT
) -> list[Issue]:
    issues: list[Issue] = []
    compiled = [
        (entry["name"], re.compile(entry["regex"])) for entry in rules["secret_patterns"]
    ]
    placeholders = [p.lower() for p in rules["secret_placeholders"]]
    for path in paths:
        target = repo_root / path
        if not target.is_file():
            continue
        if target.stat().st_size > rules["max_file_bytes"] and not (
            path.endswith("lock.yaml") or path.endswith("package-lock.json")
        ):
            issues.append(
                Issue("hygiene", path, 1, "file exceeds max_file_bytes; store large assets outside git")
            )
            continue
        if Path(path).name == ".env" or path.endswith("/.env"):
            issues.append(Issue("secret", path, 1, ".env must not be committed; use .env.example"))
        try:
            content = target.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for number, line in enumerate(content.splitlines(), start=1):
            if line.rstrip() != line:
                issues.append(Issue("hygiene", path, number, "trailing whitespace"))
            if any(line.startswith(marker) for marker in CONFLICT_MARKERS):
                issues.append(Issue("hygiene", path, number, "git conflict marker"))
            for name, pattern in compiled:
                match = pattern.search(line)
                if not match:
                    continue
                value = match.group(2) if match.lastindex and match.lastindex >= 2 else ""
                if value and any(p in value.lower() for p in placeholders):
                    continue
                issues.append(Issue("secret", path, number, f"possible {name}"))
    return issues
```

- [x] **Step 4: Jalankan test, verifikasi hijau**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: `OK` (20 tests).

- [x] **Step 5: Commit**

```bash
git add scripts/pre_commit_check.py scripts/test_pre_commit_check.py
git commit -m "feat: scan staged files for secrets and hygiene issues"
```

---

### Task 5: Wire mode `files` dan `message` ke main

**Files:**
- Modify: `scripts/pre_commit_check.py`
- Modify: `scripts/test_pre_commit_check.py`

**Interfaces:**
- Consumes: semua checker dari Task 1-4.
- Produces: `main(argv: list[str] | None = None) -> int` yang benar-benar menjalankan mode `files`/`message`.

- [x] **Step 1: Tulis test gagal untuk wiring**

Tambahkan ke `scripts/test_pre_commit_check.py`:
```python
class MainTest(unittest.TestCase):
    def test_message_mode_returns_one_for_bad_message(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "COMMIT_EDITMSG"
            path.write_text("wip: bad\n", encoding="utf-8")
            self.assertEqual(checker.main(["message", str(path)]), 1)

    def test_message_mode_returns_zero_for_good_message(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "COMMIT_EDITMSG"
            path.write_text("feat(harness): add gate\n", encoding="utf-8")
            self.assertEqual(checker.main(["message", str(path)]), 0)

    def test_files_mode_uses_staged_paths(self):
        with unittest.mock.patch.object(checker, "git", return_value="server/bad.go\n"):
            with unittest.mock.patch.object(checker, "check_loc", return_value=[checker.Issue("loc", "server/bad.go", 1, "too long")]):
                with unittest.mock.patch.object(checker, "check_secret_and_hygiene", return_value=[]):
                    self.assertEqual(checker.main(["files"]), 1)

    def test_help_returns_zero(self):
        self.assertEqual(checker.main(["--help"]), 0)
```

- [x] **Step 2: Jalankan test, verifikasi gagal**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: FAIL: `test_message_mode_returns_one_for_bad_message` mendapat 0, bukan 1.

- [x] **Step 3: Implementasi wiring di `main`**

Ganti isi `main` dengan:
```python
def staged_files() -> list[str]:
    output = git("diff", "--cached", "--name-only", "--diff-filter=ACM")
    return [line for line in output.splitlines() if line]


def run_command(cmd: list[str], cwd: Path) -> tuple[bool, str]:
    result = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    return result.returncode == 0, (result.stdout + result.stderr).strip()


def check_format_and_lint(paths: list[str]) -> tuple[list[Issue], list[Warning]]:
    issues: list[Issue] = []
    warnings: list[Warning] = []
    go_files = [p for p in paths if p.endswith(".go") and Path(p).exists()]
    if go_files:
        ok, output = run_command(["gofmt", "-l", *go_files], REPO_ROOT)
        if not ok or output:
            for line in output.splitlines():
                issues.append(Issue("gofmt", line or "<gofmt>", 1, "run gofmt -w on this file"))
        packages = sorted({str(Path(p).parent) for p in go_files})
        for package in packages:
            ok, output = run_command(["go", "vet", f"./{package}"], REPO_ROOT / "server")
            if not ok:
                issues.append(Issue("go_vet", package, 1, output or "go vet failed"))
    web_root = REPO_ROOT / "web"
    prettier = web_root / "node_modules/.bin/prettier"
    eslint = web_root / "node_modules/.bin/eslint"
    web_files = [
        p for p in paths
        if p.startswith("web/") and Path(p).suffix in {".ts", ".tsx", ".js", ".jsx", ".json", ".css"}
    ]
    if web_files and not (prettier.exists() and eslint.exists()):
        warnings.append(Warning("skipped web format/lint, web/node_modules missing"))
    else:
        relative = [str(Path(p).relative_to("web")) for p in web_files]
        if relative:
            ok, output = run_command([str(prettier), "--check", *relative], web_root)
            if not ok:
                for line in output.splitlines():
                    issues.append(Issue("prettier", line or "<prettier>", 1, "run prettier --write"))
            ok, output = run_command([str(eslint), *relative], web_root)
            if not ok:
                issues.append(Issue("eslint", "<web>", 1, output or "eslint failed"))
    return issues, warnings


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if not args or args[0] in {"-h", "--help"}:
        print("usage: pre_commit_check.py files|message [<commit-msg-file>]")
        return 0
    rules = load_rules()
    issues: list[Issue] = []
    warnings: list[Warning] = []
    mode = args[0]
    if mode == "message":
        if len(args) < 2:
            print("[FAIL] message mode requires a commit message file")
            return 1
        issues = check_message(Path(args[1]), rules)
    elif mode == "files":
        paths = staged_files()
        issues += check_loc(paths, rules)
        issues += check_secret_and_hygiene(paths, rules)
        lint_issues, warnings = check_format_and_lint(paths)
        issues += lint_issues
    else:
        print(f"[FAIL] unknown mode: {mode}")
        return 1
    for item in warnings:
        print(item.render())
    for item in issues:
        print(item.render())
    error_count = len(issues)
    print(f"{error_count} error(s), {len(warnings)} warning(s)")
    return 1 if error_count else 0
```

- [x] **Step 4: Jalankan test, verifikasi hijau**

Run: `cd /d/Github/agent-workspace/harness-go && python3 scripts/test_pre_commit_check.py`
Expected: `OK` (24 tests).

- [x] **Step 5: Commit**

```bash
git add scripts/pre_commit_check.py scripts/test_pre_commit_check.py
git commit -m "feat: wire pre-commit checker modes"
```

---

### Task 6: Pasang hook Husky dan target Makefile

**Files:**
- Create: `.husky/pre-commit`
- Create: `.husky/commit-msg`
- Modify: `web/package.json`
- Modify: `Makefile`
- Modify: `CONTRIBUTING.md`

**Interfaces:**
- Consumes: `scripts/pre_commit_check.py` dari Task 1-5.
- Produces: hook aktif setelah `pnpm install`, dan target `make check-commit`.

- [x] **Step 1: Tulis hook pre-commit**

Buat `.husky/pre-commit`:
```sh
#!/usr/bin/env bash
cd "$(dirname "$0")/.."
python3 scripts/pre_commit_check.py files
```

- [x] **Step 2: Tulis hook commit-msg**

Buat `.husky/commit-msg`:
```sh
#!/usr/bin/env bash
cd "$(dirname "$0")/.."
python3 scripts/pre_commit_check.py message "$1"
```

- [x] **Step 3: Tambah dependency Husky dan prepare script**

Di `web/package.json`, tambahkan ke `devDependencies`:
```json
"husky": "^9.1.7"
```
dan ke `scripts`:
```json
"prepare": "husky"
```
Jalankan `cd web && pnpm install` untuk memperbarui lockfile dan mengaktifkan hook.

- [x] **Step 4: Tambah target Makefile**

Tambahkan ke `Makefile` root:
```makefile
## check-commit: run the pre-commit quality gate against staged files
.PHONY: check-commit
check-commit:
	@python3 scripts/pre_commit_check.py files
	@if [ -n "$(MSG)" ]; then printf '%s\n' "$(MSG)" > .git/COMMIT_EDITMSG.tmp && python3 scripts/pre_commit_check.py message .git/COMMIT_EDITMSG.tmp && rm -f .git/COMMIT_EDITMSG.tmp; fi
```

- [x] **Step 5: Dokumentasikan di CONTRIBUTING.md**

Tambahkan bagian:
```markdown
## Pre-commit quality gate

Setelah `pnpm install`, Husky mengaktifkan dua hook: `pre-commit` (LOC 500,
gofmt, Prettier, ESLint, go vet, secret/hygiene) dan `commit-msg`
(Conventional Commits). Gate hanya memeriksa file yang di-stage. Jalankan
`make check-commit MSG="feat: contoh"` untuk verifikasi manual tanpa commit.
File di atas 500 baris yang tidak bertambah tidak ditolak; daftar
pengecualian ada di `scripts/pre_commit_rules.json`.
```

- [x] **Step 6: Verifikasi integrasi**

Run:
```bash
cd /d/Github/agent-workspace/harness-go
chmod +x .husky/pre-commit .husky/commit-msg
python3 scripts/test_pre_commit_check.py
python3 scripts/pre_commit_check.py --help
git config --get core.hooksPath || echo "hooks path not set"
```
Expected: 24 tests OK, help tampil, `core.hooksPath` = `.husky` setelah install. Jika `pnpm install` gagal karena versi, hook tetap bisa diuji manual lewat `python3 scripts/pre_commit_check.py files`.

- [x] **Step 7: Commit**

```bash
git add .husky/pre-commit .husky/commit-msg web/package.json web/pnpm-lock.yaml Makefile CONTRIBUTING.md
git commit -m "feat: install husky hooks and make target"
```

---

### Task 7: Self-review dan final verification

**Files:**
- Modify: `docs/superpowers/plans/2026-11-21-precommit-quality-gate.md` (centang semua step)

**Interfaces:**
- Consumes: semua task sebelumnya.
- Produces: plan selesai dan terverifikasi.

- [x] **Step 1: Self-review plan terhadap spec**

Checklist:
- [x] LOC 500 dengan allowlist → Task 2.
- [x] Format + lint existing → Task 5 `check_format_and_lint`.
- [x] Conventional Commits → Task 3 + Task 5 mode `message`.
- [x] Secret & hygiene → Task 4.
- [x] Husky hook → Task 6.
- [x] `make check-commit` → Task 6.
- [x] Tidak ada `lint-staged` → tidak ada task yang menambahkannya.
- [x] Tidak ada `shell=True` → semua `subprocess.run` memakai list argumen.

- [x] **Step 2: Jalankan semua verifikasi akhir**

Run:
```bash
cd /d/Github/agent-workspace/harness-go
python3 scripts/test_pre_commit_check.py
python3 scripts/pre_commit_check.py --help
git status --short
```
Expected: 24 tests OK, help tampil, working tree bersih setelah commit Task 6.

- [x] **Step 3: Commit self-review**

```bash
git add docs/superpowers/plans/2026-11-21-precommit-quality-gate.md
git commit -m "docs: complete pre-commit quality gate plan"
```

## Implementasi akhir — penyimpangan dari draft

Dokumen ini adalah rencana; bagian berikut mencatat penyempurnaan yang dilakukan saat eksekusi agar perilaku aktual tidak hilang:

- `python` dipakai dengan fallback (`python3` lalu `python`) karena `python3` tidak tersedia di shell Windows. Hook dan Makefile memakai pola `${PYTHON:-python3}` dengan fallback `python`.
- Husky 9 dipasang lewat `prepare: "cd .. && web/node_modules/.bin/husky"` di `web/package.json` karena `pnpm install` berjalan dari `web/` dan `.git` ada di root. `core.hooksPath` aktif = `.husky/_`.
- `node_bin()` di checker membaca field `bin` dari `package.json` paket dan menjalankan `node <entry>` untuk shim `.bin` bertanda `#!/bin/sh` (Windows tidak bisa mengeksekusi shim langsung).
- Regex AWS diperbaiki menjadi `\b(?:AKIA|ASIA)[A-Z0-9]{16}\b` (20 karakter total). Fixture test memakai `AKIAEXAMPLE0000000000` (placeholder) dan `AKIA1234567890ABCDEF` (positif).
- `secret_scan_allowlist` ditambahkan ke rules JSON agar file fixture test sengaja tidak memicu secret scan; ini source of truth yang sama dengan aturan lain.
- `WEB lint` dilewati dengan warning bila `web/node_modules` tidak ada, sesuai spec.
- Jumlah test akhir = 28 (draft menyebut 24 sebelum penambahan `NodeBinTest`, fixture placeholder, dan allowlist).
- `Makefile` `check-commit` menulis pesan ke `.git/precommit-msg.tmp` dan selalu membersihkannya, dengan `rc` dikembalikan benar.

