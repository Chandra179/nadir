#!/usr/bin/env python3
"""Export blind human reviews and measure judge agreement without inventing labels.

export creates packets with empty reviewer metadata and human scores. score
requires genuinely completed review files and leaves the source report intact.
The resulting calibration applies only to the exact report/model/fixture in
the manifest; thresholds are explicit engineering criteria, not certification.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
from pathlib import Path
from statistics import mean

METRICS = ("faithfulness", "answer_relevancy", "context_precision", "context_recall")
RUBRIC = {
    "faithfulness": "Fraction of claims the answer actually makes entailed by the admitted context. Brevity alone is not unfaithfulness.",
    "answer_relevancy": "How directly the answer addresses the question; do not require the reference answer's length or style.",
    "context_precision": "Fraction of admitted context relevant to the question/reference answer; empty evidence scores zero.",
    "context_recall": "Fraction of reference claims supported by admitted context; empty evidence scores zero.",
    "abstention_score": "For unsupported cases only: 1 means clearly declines to invent an answer because context cannot answer; 0 means gives an unsupported answer.",
}


def read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def evaluator_payload(report: dict) -> dict:
    """Read the new envelope or legacy native JSON; hashes cover the whole file."""
    # Native retrieval reports also have a numeric `runs` repetition count.
    if "tool" not in report and not isinstance(report.get("runs"), list):
        return report
    if report.get("schema_version") != 1 or report.get("tool") != "evaluator":
        raise ValueError("calibration requires an evaluator run report")
    runs = report.get("runs")
    if not isinstance(runs, list) or len(runs) != 1 or not isinstance(runs[0], dict) or not isinstance(runs[0].get("results"), dict):
        raise ValueError("calibration requires exactly one persisted evaluator result")
    return runs[0]["results"]


def json_bytes(value: object) -> bytes:
    return (json.dumps(value, indent=2, ensure_ascii=False) + "\n").encode()


def sha_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def write_new(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    # Existing human work must never be replaced by an empty packet.
    with path.open("xb") as handle:
        handle.write(json_bytes(value))


def immutable_packet(packet: dict) -> dict:
    return {"schema_version": packet["schema_version"], "source_report_sha256": packet["source_report_sha256"],
            "source_golden_sha256": packet["source_golden_sha256"], "rubric": packet["rubric"],
            "cases": [{key: value for key, value in case.items() if key not in ("human_scores", "rationale")}
                      for case in packet["cases"]]}


def pick_cases(results: list[dict], count: int, seed: str) -> list[dict]:
    """Round-robin strata cover terse answers, abstention and low judge scores."""
    eligible = [result for result in results if not result.get("error") and result.get("answer_status") == "success"
                and result.get("judge_status") == "success"]
    order = sorted(eligible, key=lambda result: sha_bytes(f"{seed}:{result['id']}".encode()))
    groups = [
        [result for result in order if result.get("abstention_expected")],
        [result for result in order if result.get("faithfulness", 1) < 0.5],
        [result for result in order if result.get("answer_bytes", len(result.get("answer", "").encode())) < 160],
        [result for result in order if result.get("context_truncated")],
    ]
    groups.extend([[result for result in order if result.get("type") == kind]
                   for kind in sorted({result.get("type", "") for result in order})])
    groups.append(order)
    selected = []
    seen = set()
    while len(selected) < min(count, len(order)):
        progress = False
        for group in groups:
            while group and group[0]["id"] in seen:
                group.pop(0)
            if group and len(selected) < count:
                result = group.pop(0)
                selected.append(result)
                seen.add(result["id"])
                progress = True
        if not progress:
            break
    return selected


def export(report_path: Path, golden_path: Path, output_dir: Path, count: int = 40, seed: str = "nadir-judge-calibration-v1") -> dict:
    if count < 1:
        raise ValueError("sample size must be positive")
    report = evaluator_payload(read_json(report_path))
    golden = read_json(golden_path)
    report_sha = sha_bytes(report_path.read_bytes())
    golden_sha = sha_bytes(golden_path.read_bytes())
    declared = report.get("provenance", {}).get("golden_sha256")
    if declared != golden_sha:
        raise ValueError("report golden_sha256 must match the exact supplied fixture")
    generation = report.get("generation")
    if not generation:
        raise ValueError("report has no generation evaluation")
    annotations = {query["id"]: query for query in golden["queries"]}
    selected = pick_cases(generation["per_query"], count, seed)
    if not selected:
        raise ValueError("report has no successfully judged answers")
    packet = {"schema_version": 1, "source_report_sha256": report_sha, "source_golden_sha256": golden_sha,
              "reviewer": {"id": "", "human": None, "independent": None, "verification_ref": "", "reviewed_at": ""},
              "rubric": RUBRIC, "cases": []}
    for result in selected:
        query = annotations.get(result["id"])
        if not query or query["query"] != result["query"]:
            raise ValueError(f"{result['id']}: fixture/query mismatch")
        if not result.get("answer") or "admitted_context" not in result:
            raise ValueError("report needs persisted answers and exact admitted_context; run the current evaluator")
        required = (*METRICS, "abstention_score") if result.get("abstention_expected") else METRICS
        packet["cases"].append({"id": result["id"], "query": query["query"], "type": query["type"],
                                "faithfulness_label": query["faithfulness_label"], "expected_answer": query["expected_answer"],
                                "required_claims": query["required_claims"], "answer": result["answer"],
                                "admitted_context": result["admitted_context"], "evidence": result.get("evidence", []),
                                "human_scores": {metric: None for metric in required}, "rationale": ""})
    manifest = {"schema_version": 1, "source_report": str(report_path), "source_report_sha256": report_sha,
                "source_golden": str(golden_path), "source_golden_sha256": golden_sha,
                "answer_model": generation["answer_model"], "judge_model": generation["judge_model"],
                "model_fingerprints": generation.get("model_fingerprints", []),
                "sampling": "deterministic round-robin abstention, low-faithfulness, terse, truncated, query-type strata",
                "seed": seed, "case_ids": [result["id"] for result in selected],
                "packet_content_sha256": sha_bytes(json_bytes(immutable_packet(packet))),
                "calibration_status": "pending_human_review"}
    if any((output_dir / name).exists() for name in ("manifest.json", "reviewer-a.json", "reviewer-b.json")):
        raise ValueError("output files already exist; choose a new directory to preserve any human reviews")
    write_new(output_dir / "manifest.json", manifest)
    write_new(output_dir / "reviewer-a.json", packet)
    write_new(output_dir / "reviewer-b.json", packet)
    return manifest


def percentile(values: list[float], p: float) -> float:
    values = sorted(values)
    at = p * (len(values) - 1)
    lo, hi = math.floor(at), math.ceil(at)
    return values[lo] + (values[hi] - values[lo]) * (at - lo)


def agreement(judge: list[float], human: list[float]) -> dict:
    absolute = [abs(left - right) for left, right in zip(judge, human)]
    left_mean, right_mean = mean(judge), mean(human)
    numerator = sum((left - left_mean) * (right - right_mean) for left, right in zip(judge, human))
    denominator = math.sqrt(sum((value - left_mean) ** 2 for value in judge) * sum((value - right_mean) ** 2 for value in human))
    return {"cases": len(absolute), "mean_absolute_error": mean(absolute),
            "median_absolute_error": percentile(absolute, 0.5), "p95_absolute_error": percentile(absolute, 0.95),
            "mean_judge_minus_human": mean([left - right for left, right in zip(judge, human)]),
            "pearson_correlation": numerator / denominator if denominator else None}


def score(manifest_path: Path, review_paths: list[Path], min_cases: int = 20, max_mae: float = 0.15, max_p95: float = 0.35) -> dict:
    manifest = read_json(manifest_path)
    report_path = Path(manifest["source_report"])
    if sha_bytes(report_path.read_bytes()) != manifest["source_report_sha256"]:
        raise ValueError("source report changed since packet export")
    golden_path = Path(manifest["source_golden"])
    if sha_bytes(golden_path.read_bytes()) != manifest["source_golden_sha256"]:
        raise ValueError("source fixture changed since packet export")
    generation = evaluator_payload(read_json(report_path))["generation"]
    judged = {result["id"]: result for result in generation["per_query"]}
    reviews = []
    reviewer_ids = set()
    for path in review_paths:
        packet = read_json(path)
        if sha_bytes(json_bytes(immutable_packet(packet))) != manifest["packet_content_sha256"]:
            raise ValueError(f"{path}: immutable review content differs from the exported packet")
        reviewer = packet["reviewer"]
        if not reviewer.get("id", "").strip() or reviewer["id"] in reviewer_ids:
            raise ValueError(f"{path}: reviewer IDs must be nonempty and distinct")
        if reviewer.get("human") is not True or reviewer.get("independent") is not True or not reviewer.get("verification_ref", "").strip():
            raise ValueError(f"{path}: requires explicit independent human attestation and verification_ref")
        from datetime import datetime
        try:
            reviewed = datetime.fromisoformat(reviewer.get("reviewed_at", "").replace("Z", "+00:00"))
            if reviewed.tzinfo is None:
                raise ValueError("timezone missing")
        except ValueError as error:
            raise ValueError(f"{path}: reviewed_at requires an ISO timestamp with timezone") from error
        reviewer_ids.add(reviewer["id"])
        by_id = {}
        for case in packet["cases"]:
            required = (*METRICS, "abstention_score") if judged[case["id"]].get("abstention_expected") else METRICS
            for metric in required:
                value = case.get("human_scores", {}).get(metric)
                if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 <= value <= 1:
                    raise ValueError(f"{path}: {case['id']}/{metric} requires a human score in [0,1]")
            if not case.get("rationale", "").strip():
                raise ValueError(f"{path}: {case['id']} requires a review rationale")
            by_id[case["id"]] = case["human_scores"]
        reviews.append({"reviewer": reviewer, "file_sha256": sha_bytes(path.read_bytes()), "scores": by_id})
    if not reviews:
        raise ValueError("at least one completed independent human review is required")
    metrics = {}
    human_agreement = {}
    for metric in (*METRICS, "abstention_score"):
        ids = [case_id for case_id in manifest["case_ids"] if metric in reviews[0]["scores"][case_id]]
        if not ids:
            continue
        metrics[metric] = agreement([judged[case_id][metric] for case_id in ids],
                                    [mean([review["scores"][case_id][metric] for review in reviews]) for case_id in ids])
        if len(reviews) > 1:
            spreads = [max(review["scores"][case_id][metric] for review in reviews)
                       - min(review["scores"][case_id][metric] for review in reviews) for case_id in ids]
            human_agreement[metric] = {"mean_reviewer_spread": mean(spreads), "p95_reviewer_spread": percentile(spreads, 0.95)}
    sufficient = len(reviews) >= 2 and len(manifest["case_ids"]) >= min_cases
    within = all(value["mean_absolute_error"] <= max_mae and value["p95_absolute_error"] <= max_p95 for value in metrics.values())
    human_consistent = all(value["mean_reviewer_spread"] <= max_mae and value["p95_reviewer_spread"] <= max_p95 for value in human_agreement.values())
    return {"schema_version": 1, "source_report_sha256": manifest["source_report_sha256"],
            "source_golden_sha256": manifest["source_golden_sha256"], "manifest_sha256": sha_bytes(manifest_path.read_bytes()),
            "answer_model": manifest["answer_model"], "judge_model": manifest["judge_model"],
            "model_fingerprints": manifest["model_fingerprints"], "reviewed_cases": len(manifest["case_ids"]),
            "reviewers": [{"metadata": review["reviewer"], "file_sha256": review["file_sha256"]} for review in reviews],
            "thresholds": {"minimum_cases": min_cases, "minimum_reviewers": 2, "max_mae": max_mae, "max_p95_absolute_error": max_p95},
            "judge_agreement": metrics, "human_agreement": human_agreement,
            "status": "reviewed_within_declared_thresholds" if sufficient and within and human_consistent else "needs_more_review_or_judge_calibration",
            "release_gate": False,
            "scope": "Only these sampled answers, fixture, and observed model fingerprints; independent attestation is supplied by reviewers, not verified by this script."}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    exporter = sub.add_parser("export")
    exporter.add_argument("--report", type=Path, required=True)
    exporter.add_argument("--golden", type=Path, required=True)
    exporter.add_argument("--output-dir", type=Path, required=True)
    exporter.add_argument("--sample-size", type=int, default=40)
    exporter.add_argument("--seed", default="nadir-judge-calibration-v1")
    scorer = sub.add_parser("score")
    scorer.add_argument("--manifest", type=Path, required=True)
    scorer.add_argument("--reviews", type=Path, nargs="+", required=True)
    scorer.add_argument("--output", type=Path, required=True)
    scorer.add_argument("--min-cases", type=int, default=20)
    scorer.add_argument("--max-mae", type=float, default=0.15)
    scorer.add_argument("--max-p95", type=float, default=0.35)
    args = parser.parse_args()
    try:
        if args.command == "export":
            result = export(args.report, args.golden, args.output_dir, args.sample_size, args.seed)
            print(f"exported {len(result['case_ids'])} blind cases; status=pending_human_review")
        else:
            if args.min_cases < 1 or not 0 <= args.max_mae <= 1 or not 0 <= args.max_p95 <= 1:
                raise ValueError("minimum cases must be positive and error thresholds must be in [0,1]")
            result = score(args.manifest, args.reviews, args.min_cases, args.max_mae, args.max_p95)
            write_new(args.output, result)
            print(f"scored {result['reviewed_cases']} cases; status={result['status']}")
    except (ValueError, KeyError, OSError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
