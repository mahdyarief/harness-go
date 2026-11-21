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
