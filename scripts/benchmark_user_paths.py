#!/usr/bin/env python3
"""Probe semantic-cache reuse and follow-up rewriting through the public API.

Annotations are synthetic retrieval expectations, not human answer judgments.
Only sessions created by this run are deleted; document/cache collections are
never reset. Use an isolated API to avoid other clients warming the cache.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import time
import urllib.error
import urllib.parse
import urllib.request

from benchmark_load import percentile, read_stream_result, request_json


def evidence_scores(results: list[dict], relevant: list[dict]) -> dict:
    """Count each annotation once, even when multiple chunks match it."""
    matched = set()
    relevant_chunks = 0
    for chunk in results:
        hits = {
            index for index, label in enumerate(relevant)
            if pathlib.PurePosixPath(chunk.get("file_path", "")).name == label["file"]
            and label.get("contains", "").casefold() in chunk.get("text", "").casefold()
        }
        matched.update(hits)
        relevant_chunks += bool(hits)
    return {
        "hit": bool(matched),
        "precision": relevant_chunks / len(results) if results else 0.0,
        "recall": len(matched) / len(relevant) if relevant else 0.0,
    }


def rewrite_matches(query: str, required_any: list[str]) -> bool:
    # This is an auditable lexical drift diagnostic, not an LLM quality score.
    return any(term.casefold() in query.casefold() for term in required_any)


class Probe:
    def __init__(self, base_url: str, timeout: float, top_k: int, generate: bool):
        self.base = base_url.rstrip("/")
        self.timeout = timeout
        self.top_k = top_k
        self.generate = generate
        self.sessions: set[str] = set()

    def turn(self, query: str, *, skip_cache=False, session_id="", generate=False) -> dict:
        started = time.monotonic()
        _, body = request_json(self.base + "/api/v1/turns", {
            "query": query, "top_k": self.top_k, "skip_cache": skip_cache,
            "session_id": session_id, "generate": generate,
        }, self.timeout)
        turn = json.loads(body)
        if turn.get("session_id"):
            self.sessions.add(turn["session_id"])
        if turn.get("error") or turn.get("generate_error"):
            raise ValueError(turn.get("error") or turn["generate_error"])
        if generate:
            stream_url = turn.get("stream_url")
            if not stream_url and turn.get("has_answer") and turn.get("answer"):
                turn["first_token_ms"] = None
                turn["answer_delivery"] = "immediate"
            elif not stream_url:
                raise ValueError("generation requested but no stream was started")
            else:
                retrieval_ms = (time.monotonic() - started) * 1000
                stream = read_stream_result(self.base + stream_url, self.timeout)
                turn["answer"] = stream.answer
                turn["first_token_ms"] = None if stream.first_token_ms is None else round(retrieval_ms + stream.first_token_ms, 3)
                turn["answer_delivery"] = "stream"
        turn["measured_latency_ms"] = round((time.monotonic() - started) * 1000, 3)
        return turn

    def wait_saved(self, session_id: str, query: str) -> None:
        deadline = time.monotonic() + min(self.timeout, 15)
        while True:
            request = urllib.request.Request(self.base + "/api/v1/sessions/" + urllib.parse.quote(session_id, safe=""))
            try:
                with urllib.request.urlopen(request, timeout=self.timeout) as response:
                    session = json.load(response)
                if any(turn.get("query") == query for turn in session.get("turns", [])):
                    return
            except urllib.error.HTTPError as exc:
                if exc.code != 404:
                    raise
            if time.monotonic() >= deadline:
                raise ValueError("seed turn was not persisted; rewriting requires enabled history")
            time.sleep(0.1)

    def cache_case(self, case: dict, cache_wait: float) -> dict:
        fresh = self.turn(case["probe"], skip_cache=True)
        seed = self.turn(case["seed"])
        deadline = time.monotonic() + cache_wait
        warm = seed
        while not warm.get("from_cache") and time.monotonic() < deadline:
            time.sleep(0.1)
            warm = self.turn(case["seed"])
        probe = self.turn(case["probe"])
        fresh_score = evidence_scores(fresh.get("results", []), case["relevant"])
        probe_score = evidence_scores(probe.get("results", []), case["relevant"])
        return {
            "id": case["id"], "seed_ready": bool(warm.get("from_cache")),
            "from_cache": bool(probe.get("from_cache")), "fresh_scores": fresh_score,
            "probe_scores": probe_score,
            "cache_regression": bool(probe.get("from_cache") and fresh_score["hit"] and not probe_score["hit"]),
            "fresh": fresh, "seed": seed, "probe": probe,
        }

    def rewrite_case(self, case: dict) -> dict:
        fresh = self.turn(case["standalone"], skip_cache=True)
        seed = self.turn(case["seed"], skip_cache=True, generate=self.generate)
        if not seed.get("session_id"):
            raise ValueError("history is disabled; follow-up rewriting cannot be measured")
        self.wait_saved(seed["session_id"], case["seed"])
        followup = self.turn(case["followup"], skip_cache=True, session_id=seed["session_id"], generate=self.generate)
        rewritten = followup.get("rewritten_query", "")
        fresh_score = evidence_scores(fresh.get("results", []), case["relevant"])
        followup_score = evidence_scores(followup.get("results", []), case["relevant"])
        return {
            "id": case["id"], "rewrite_observed": bool(rewritten),
            "topic_matches": rewrite_matches(rewritten or case["followup"], case["required_any"]),
            "fresh_scores": fresh_score, "followup_scores": followup_score,
            "retrieval_regression": bool(fresh_score["hit"] and not followup_score["hit"]),
            "fresh": fresh, "seed": seed, "followup": followup,
        }

    def cleanup(self) -> list[str]:
        errors = []
        for session in sorted(self.sessions):
            request = urllib.request.Request(self.base + "/api/v1/sessions/" + urllib.parse.quote(session, safe=""), method="DELETE")
            try:
                with urllib.request.urlopen(request, timeout=self.timeout):
                    pass
            except OSError as exc:
                errors.append(f"{session}: {exc}")
        return errors


def summarize(cache: list[dict], rewriting: list[dict]) -> dict:
    hits = [case for case in cache if case.get("from_cache")]
    measured_rewrites = [case for case in rewriting if "followup" in case]
    return {
        "cache_cases": len(cache), "cache_hits": len(hits),
        "cache_coverage": len(hits) / len(cache) if cache else None,
        "cache_hit_correctness": sum(case["probe_scores"]["hit"] for case in hits) / len(hits) if hits else None,
        "cache_retrieval_regressions": sum(case.get("cache_regression", False) for case in cache),
        "cache_probe_p50_ms": percentile([case["probe"]["measured_latency_ms"] for case in cache if "probe" in case], 0.5),
        "rewrite_cases": len(rewriting), "rewrites_observed": sum(case.get("rewrite_observed", False) for case in rewriting),
        "rewrite_topic_matches": sum(case.get("topic_matches", False) for case in rewriting),
        "rewrite_retrieval_regressions": sum(case.get("retrieval_regression", False) for case in rewriting),
        "followup_p50_ms": percentile([case["followup"]["measured_latency_ms"] for case in measured_rewrites], 0.5),
        "operational_failures": sum("error" in case for case in cache + rewriting),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8100")
    parser.add_argument("--fixture", type=pathlib.Path, default=pathlib.Path("test/evaluation/user-paths.json"))
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--cache-wait", type=float, default=5)
    parser.add_argument("--top-k", type=int, default=5)
    parser.add_argument("--generate", action="store_true", help="also generate seed/follow-up answers and persist them for human review")
    parser.add_argument("--profile-label", default="unspecified")
    parser.add_argument("--provenance", type=pathlib.Path)
    parser.add_argument("--json-out", type=pathlib.Path)
    args = parser.parse_args()
    if args.top_k <= 0 or args.timeout <= 0 or args.cache_wait < 0:
        parser.error("top-k and timeout must be positive; cache-wait must be nonnegative")
    raw = args.fixture.read_bytes()
    fixture = json.loads(raw)
    report = {
        "schema_version": 1, "measured_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "fixture_sha256": hashlib.sha256(raw).hexdigest(), "metadata": fixture["metadata"],
        "profile_label": args.profile_label, "base_url": args.base_url, "top_k": args.top_k,
        "generated_history": args.generate,
        "provenance": json.loads(args.provenance.read_text()) if args.provenance else None,
        "interpretation": "Synthetic retrieval labels; cache_hit_correctness checks annotated evidence, not answer faithfulness. Topic matches are lexical rewrite diagnostics. Not a release gate.",
        "cache": [], "rewriting": [],
    }
    probe = Probe(args.base_url, args.timeout, args.top_k, args.generate)
    try:
        for kind in ["cache", "rewriting"]:
            for case in fixture[kind]:
                try:
                    result = probe.cache_case(case, args.cache_wait) if kind == "cache" else probe.rewrite_case(case)
                except (OSError, ValueError) as exc:
                    result = {"id": case["id"], "error": str(exc)}
                report[kind].append(result)
    finally:
        report["cleanup_errors"] = probe.cleanup()
    report["summary"] = summarize(report["cache"], report["rewriting"])
    encoded = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
    if args.json_out:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(encoded, encoding="utf-8")
    print(json.dumps(report["summary"], indent=2))
    return int(bool(report["summary"]["operational_failures"] or report["cleanup_errors"]))


if __name__ == "__main__":
    raise SystemExit(main())
