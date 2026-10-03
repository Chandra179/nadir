import importlib.util
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).parent))
spec = importlib.util.spec_from_file_location("run_daily_use", pathlib.Path(__file__).with_name("run_daily_use.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class DailyUseTest(unittest.TestCase):
    def test_citation_check_uses_admitted_mapping_and_detects_unknown_number(self):
        turn = {"answer": "Correct [2], invented [9]", "citations": [{"number": 2, "file_path": "samples/a.md", "text": "actual evidence"}]}
        check = runner.check_citations(turn, [{"file": "a.md", "contains": "actual"}])
        self.assertTrue(check["expected_evidence_admitted"])
        self.assertTrue(check["expected_evidence_cited"])
        self.assertEqual(check["unmapped"], [9])

    def test_admitted_but_uncited_evidence_does_not_count_as_cited(self):
        turn = {"answer": "An answer", "citations": [{"number": 1, "file_path": "a.md", "text": "evidence"}]}
        check = runner.check_citations(turn, [{"file": "a.md", "contains": "evidence"}])
        self.assertTrue(check["expected_evidence_admitted"])
        self.assertFalse(check["expected_evidence_cited"])
