import json
import sys
import tempfile
import unittest
import unittest.mock
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
        target.write_text("a\n" * 8, encoding="utf-8")
        with unittest.mock.patch.object(checker, "git", return_value="a\n" * 6):
            issues = checker.check_loc(["server/grow.go"], self.rules, root)
        self.assertEqual(len(issues), 1)

    def test_large_existing_file_not_growing_passes(self):
        root = self.make_repo()
        target = root / "server/old.go"
        target.write_text("a\n" * 800, encoding="utf-8")
        with unittest.mock.patch.object(checker, "git", return_value="a\n" * 800):
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


if __name__ == "__main__":
    unittest.main()
