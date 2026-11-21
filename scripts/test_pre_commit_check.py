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
