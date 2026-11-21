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


if __name__ == "__main__":
    unittest.main()
