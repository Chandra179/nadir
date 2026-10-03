#!/usr/bin/env python3
"""Run saved simulated user questions through the real chat API.

Answers and evidence are saved for agent or human review. Structural checks
are reported separately from correctness; this runner never invents a human
review, independent calibration, or production release gate.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import re
import subprocess

from benchmark_user_paths import Probe, evidence_scores
from benchmark_load import percentile


def check_citations(turn: dict, relevant: list[dict]) -> dict:
    citations = turn.get("citations", [])
    used = sorted({int(number) for group in re.findall(r"\[([0-9, ]+)\]", turn.get("answer", "")) for number in re.findall(r"\d+", group)})
    mapped = {citation["number"] for citation in citations}
    admitted = evidence_scores(citations, relevant)
    cited = evidence_scores([citation for citation in citations if citation["number"] in used], relevant)
    return {"used": used, "unmapped": sorted(set(used) - mapped),
            "expected_evidence_admitted": admitted["hit"], "expected_evidence_cited": cited["hit"]}


def run(args) -> dict:
    raw = args.fixture.read_bytes()
    fixture = json.loads(raw)
    for source in fixture["metadata"]["corpus"]:
        if hashlib.sha256(pathlib.Path(source["path"]).read_bytes()).hexdigest() != source["sha256"]:
            raise ValueError(f"corpus changed after question authoring: {source['path']}")
    report = {"schema_version": 1, "measured_at": dt.datetime.now(dt.timezone.utc).isoformat(),
              "fixture": str(args.fixture), "fixture_sha256": hashlib.sha256(raw).hexdigest(),
              "metadata": fixture["metadata"], "base_url": args.base_url,
              "git_head": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
              "provenance": json.loads(args.provenance.read_text()) if args.provenance else None,
              "review_status": "pending_agent_or_human_review", "release_gate": False,
              "cases": [], "cleanup_errors": []}
    args.report.parent.mkdir(parents=True, exist_ok=True)
    with args.report.open("x", encoding="utf-8") as output:
        json.dump(report, output, indent=2, ensure_ascii=False)
    probe = Probe(args.base_url, args.timeout, args.top_k, True)
    by_id = {}
    try:
        for case in fixture["cases"]:
            result = {"id": case["id"], "kind": case["kind"], "query": case["query"]}
            try:
                seed = by_id.get(case.get("session_from"))
                if case.get("session_from") and (not seed or not seed.get("turn", {}).get("session_id")):
                    raise ValueError("seed session unavailable")
                if seed:
                    probe.wait_saved(seed["turn"]["session_id"], seed["query"])
                turn = probe.turn(case["query"], skip_cache=True, session_id=seed["turn"]["session_id"] if seed else "", generate=True)
                result["turn"] = turn
                result["citation_checks"] = check_citations(turn, case["relevant"])
            except (OSError, ValueError) as error:
                result["error"] = str(error)
            report["cases"].append(result)
            by_id[case["id"]] = result
            args.report.write_text(json.dumps(report, indent=2, ensure_ascii=False)+"\n", encoding="utf-8")
            print(case["id"], "error" if "error" in result else "recorded", flush=True)
    finally:
        if not args.keep_sessions:
            report["cleanup_errors"] = probe.cleanup()
        report["sessions_retained"] = args.keep_sessions
        measured = [case["turn"] for case in report["cases"] if "turn" in case]
        report["summary"] = {
            "cases": len(report["cases"]), "operational_failures": sum("error" in case for case in report["cases"]),
            "unmapped_citation_cases": sum(bool(case.get("citation_checks", {}).get("unmapped")) for case in report["cases"]),
            "expected_evidence_admitted": sum(case.get("citation_checks", {}).get("expected_evidence_admitted", False) for case in report["cases"]),
            "immediate_answers": sum(turn.get("answer_delivery") == "immediate" for turn in measured),
            "first_token_p50_ms": percentile([turn["first_token_ms"] for turn in measured if turn.get("first_token_ms") is not None], 0.5),
            "first_token_p95_ms": percentile([turn["first_token_ms"] for turn in measured if turn.get("first_token_ms") is not None], 0.95),
        }
        args.report.write_text(json.dumps(report, indent=2, ensure_ascii=False)+"\n", encoding="utf-8")
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8100")
    parser.add_argument("--fixture", type=pathlib.Path, default=pathlib.Path("test/evaluation/daily-use-questions.json"))
    parser.add_argument("--report", type=pathlib.Path, required=True)
    parser.add_argument("--provenance", type=pathlib.Path)
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--top-k", type=int, default=5)
    parser.add_argument("--keep-sessions", action="store_true")
    args = parser.parse_args()
    if args.top_k < 1 or args.timeout <= 0:
        parser.error("top-k and timeout must be positive")
    report = run(args)
    print(json.dumps(report["summary"], indent=2))
    return int(bool(report["summary"]["operational_failures"] or report["cleanup_errors"]))


if __name__ == "__main__":
    raise SystemExit(main())
