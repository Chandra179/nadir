import importlib.util
import pathlib
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).parent))
spec = importlib.util.spec_from_file_location("benchmark_user_paths", pathlib.Path(__file__).with_name("benchmark_user_paths.py"))
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)


class UserPathProbeTest(unittest.TestCase):
    def test_duplicate_chunks_do_not_inflate_recall(self):
        chunk = {"file_path": "samples/calculus.md", "text": "derivative sine"}
        score = probe.evidence_scores([chunk, chunk], [{"file": "calculus.md", "contains": "sine"}, {"file": "calculus.md", "contains": "cosine"}])
        self.assertEqual(score["recall"], 0.5)
        self.assertEqual(score["precision"], 1)

    def test_fresh_baseline_is_uncached_and_does_not_seed_probe(self):
        client = probe.Probe("http://test", 1, 5, False)
        results = [{"results": [{"file_path": "a.md", "text": "target"}]}, {"from_cache": True}, {"from_cache": True, "results": [{"file_path": "b.md", "text": "wrong"}]}]
        with patch.object(client, "turn", side_effect=results) as turn:
            result = client.cache_case({"id": "x", "seed": "s", "probe": "p", "relevant": [{"file": "a.md", "contains": "target"}]}, 0)
        self.assertEqual(turn.call_args_list[0].kwargs, {"skip_cache": True})
        self.assertTrue(result["cache_regression"])

    def test_cleanup_only_deletes_owned_sessions(self):
        client = probe.Probe("http://test", 1, 5, False)
        client.sessions = {"owned"}
        with patch.object(probe.urllib.request, "urlopen") as request:
            self.assertEqual(client.cleanup(), [])
        actual = request.call_args.args[0]
        self.assertEqual(actual.full_url, "http://test/api/v1/sessions/owned")
        self.assertEqual(actual.method, "DELETE")

    def test_summary_keeps_zero_hits_unknown_and_errors_visible(self):
        result = probe.summarize([{"error": "unavailable"}], [])
        self.assertIsNone(result["cache_hit_correctness"])
        self.assertEqual(result["operational_failures"], 1)


if __name__ == "__main__":
    unittest.main()
