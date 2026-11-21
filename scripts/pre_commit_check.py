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


CONFLICT_MARKERS = ("<<<<<<<", "=======", ">>>>>>>")


def check_secret_and_hygiene(
    paths: list[str], rules: dict, repo_root: Path = REPO_ROOT
) -> list[Issue]:
    issues: list[Issue] = []
    compiled = [
        (entry["name"], re.compile(entry["regex"])) for entry in rules["secret_patterns"]
    ]
    placeholders = [p.lower() for p in rules["secret_placeholders"]]
    secret_allowlist = rules.get("secret_scan_allowlist", [])
    for path in paths:
        target = repo_root / path
        if not target.is_file():
            continue
        normalized = path.replace("\\", "/")
        scan_secrets = not any(fnmatch(normalized, p) for p in secret_allowlist)
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
            if scan_secrets:
                for name, pattern in compiled:
                    match = pattern.search(line)
                    if not match:
                        continue
                    value = match.group(2) if match.lastindex and match.lastindex >= 2 else ""
                    if value and any(p in value.lower() for p in placeholders):
                        continue
                    issues.append(Issue("secret", path, number, f"possible {name}"))
    return issues


def staged_files() -> list[str]:
    output = git("diff", "--cached", "--name-only", "--diff-filter=ACM")
    return [line for line in output.splitlines() if line]


def run_command(cmd: list[str], cwd: Path) -> tuple[bool, str]:
    result = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    return result.returncode == 0, (result.stdout + result.stderr).strip()


def node_bin(tool: Path, web_root: Path) -> list[str]:
    if tool.exists() and tool.read_bytes()[:2] == b"#!":
        package = "prettier" if "prettier" in tool.name else "eslint"
        package_root = web_root / "node_modules" / package
        manifest = package_root / "package.json"
        bin_field = json.loads(manifest.read_text(encoding="utf-8"))["bin"]
        entry = bin_field[package] if isinstance(bin_field, dict) else bin_field
        return ["node", str(package_root / entry)]
    return [str(tool)]


def check_web_lint(
    paths: list[str], web_root: Path, prettier: Path, eslint: Path
) -> tuple[list[Issue], list[Warning]]:
    issues: list[Issue] = []
    relative = [str(Path(p).relative_to("web")) for p in paths]
    ok, output = run_command([*node_bin(prettier, web_root), "--check", *relative], web_root)
    if not ok:
        for line in output.splitlines():
            issues.append(Issue("prettier", line or "<prettier>", 1, "run prettier --write"))
    ok, output = run_command([*node_bin(eslint, web_root), *relative], web_root)
    if not ok:
        issues.append(Issue("eslint", "<web>", 1, output or "eslint failed"))
    return issues, []


def check_format_and_lint(paths: list[str]) -> tuple[list[Issue], list[Warning]]:
    issues: list[Issue] = []
    warnings: list[Warning] = []
    go_files = [p for p in paths if p.endswith(".go") and (REPO_ROOT / p).exists()]
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
    elif web_files:
        web_issues, _ = check_web_lint(web_files, web_root, prettier, eslint)
        issues += web_issues
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


if __name__ == "__main__":
    raise SystemExit(main())
