"""Capture first, then score; persist partial evidence on every exit path."""

import asyncio
import csv
import datetime as dt
import hashlib
import importlib.metadata
import math
from pathlib import Path
import uuid

from eval.api import ApiClient, contexts
from eval.dataset import dataset_snapshot, load_capture
from eval.judge import METRICS, RagasJudge
from eval.report import artifact, atomic_json, git_metadata, now, write_report


class Run:
    def __init__(self, phase, inputs, output=None):
        self.path = Path(output) if output else Path(".local/evaluation") / (
            dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ") + "-" + uuid.uuid4().hex[:8])
        self.path.mkdir(parents=True, exist_ok=False)
        revision, dirty = git_metadata()
        self.record = {"run_id": uuid.uuid4().hex, "run_type": phase, "status": "interrupted",
                       "started_at": now(), "ended_at": None, "git_revision": revision,
                       "inputs": inputs, "provenance": {"framework": "ragas", "framework_version": "0.4.3",
                           "phase": phase, "producer": {"entrypoint": "python -m eval", "working_tree_dirty": dirty}},
                       "summary": {}, "results": {"samples": []}, "errors": [], "artifacts": []}
        self.save()
        print(f"Evaluation reports: {self.path}", flush=True)

    def save(self):
        write_report(self.path / "report.json", {"schema_version": 1, "tool": "evaluator", "runs": [self.record]})

    def finish(self, interrupted=False):
        self.record["status"] = "interrupted" if interrupted else "failed" if self.record["errors"] else "completed"
        self.record["ended_at"] = now()
        self.save()


async def collect(run, dataset, host, top_k=5, timeout=120, repetitions=1, *, api_factory=ApiClient):
    capture = {"schema_version": 1, "producer": "nadir-eval", "created_at": now(),
               "provenance": {}, "samples": []}
    path = run.path / "capture.json"
    samples, dataset_info = dataset_snapshot(dataset)
    api = api_factory(host, timeout)
    try:
        capture["provenance"] = {"dataset": dataset_info, "api": await api.provenance(),
                                 "top_k": top_k, "repetitions": repetitions,
                                 "workflow_timeout_seconds": timeout, "git_revision": run.record["git_revision"]}
        run.record["provenance"].update(capture["provenance"])
        for repetition in range(1, repetitions + 1):
            for sample in samples:
                row = {**sample, "sample_id": f"{sample['id']}:{repetition}", "repetition": repetition,
                       "status": "failed", "errors": [], "started_at": now()}
                capture["samples"].append(row)
                try:
                    turn, answer = await api.turn(sample["user_input"], top_k)
                    retrieved, admitted = contexts(turn)
                    row.update(response=answer, retrieved_contexts=retrieved, admitted_contexts=admitted,
                               api_turn=turn, status="completed")
                except Exception as error:
                    message = f"{row['sample_id']}: {type(error).__name__}: {error}"
                    row["errors"].append(message)
                    run.record["errors"].append(message)
                finally:
                    row["ended_at"] = now()
                    # Checkpoint each sample; no score phase ever edits this artifact.
                    atomic_json(path, capture)
    finally:
        try:
            run.record["errors"].extend(await api.cleanup())
        finally:
            await api.close()
            atomic_json(path, capture)
            run.record["artifacts"].append(artifact(path, run.path))
            run.record["summary"]["collected"] = sum(x["status"] == "completed" for x in capture["samples"])
            run.record["summary"]["collection_failed"] = sum(x["status"] != "completed" for x in capture["samples"])
            run.record["results"]["capture"] = str(path.resolve())
            run.save()
    return path


def save_scores(run):
    path = run.path / "scores.csv"
    with path.open("w", newline="", encoding="utf-8") as stream:
        fields = ["sample_id", "metric", "value", "status", "reason"]
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        for sample in run.record["results"]["samples"]:
            for metric, score in sample["metrics"].items():
                writer.writerow({"sample_id": sample["sample_id"], "metric": metric, **score})
    summary = {}
    for metric in METRICS:
        scores = [x["metrics"][metric] for x in run.record["results"]["samples"] if metric in x["metrics"]]
        values = [x["value"] for x in scores if x["status"] == "scored"]
        summary[metric] = {"mean": sum(values) / len(values) if values else None, "scored": len(values),
                           "undefined": sum(x["status"] == "undefined" for x in scores),
                           "failed": sum(x["status"] == "failed" for x in scores),
                           "pending": len(run.record["results"]["samples"]) - len(scores)}
    run.record["summary"]["metrics"] = summary
    run.record["summary"]["scored_samples"] = sum(any(s["status"] == "scored" for s in x["metrics"].values())
                                                  for x in run.record["results"]["samples"])
    run.record["artifacts"] = [a for a in run.record["artifacts"] if a["path"] != "scores.csv"] + [artifact(path, run.path)]
    run.save()


async def score(run, path, config, *, judge_factory=RagasJudge):
    capture = load_capture(path)
    original_hash = hashlib.sha256(Path(path).read_bytes()).hexdigest()
    run.record["provenance"].update(judge=config.provenance(), capture=artifact(path),
                                    collection=capture["provenance"], installed_ragas=importlib.metadata.version("ragas"))
    run.record["errors"].extend(error for row in capture["samples"] for error in row["errors"]
                                if error not in run.record["errors"])
    run.record["results"]["samples"] = [{**row, "metrics": {}} for row in capture["samples"] if row["status"] == "completed"]
    judge = None
    try:
        judge = judge_factory(config)
        for row in run.record["results"]["samples"]:
            for name in METRICS:
                result = {"value": None, "status": "undefined", "reason": None}
                contexts_key = "admitted_contexts" if name == "faithfulness" else "retrieved_contexts"
                if name != "factual_correctness" and not any(x.strip() for x in row[contexts_key]):
                    result["reason"] = f"no {contexts_key}; metric is not applicable"
                else:
                    try:
                        async with asyncio.timeout(config.timeout):
                            value = await judge.score(name, row)
                        if math.isfinite(value):
                            if not 0 <= value <= 1:
                                raise ValueError("Ragas returned a score outside [0,1]")
                            result.update(value=value, status="scored")
                        else:
                            result["reason"] = "Ragas returned an undefined score (for example, no extracted claims)"
                    except Exception as error:
                        message = f"{row['sample_id']} {name}: {type(error).__name__}: {error}"
                        result.update(status="failed", reason=message)
                        run.record["errors"].append(message)
                row["metrics"][name] = result
                save_scores(run)
    finally:
        try:
            if judge is not None:
                await judge.close()
        finally:
            save_scores(run)
            if hashlib.sha256(Path(path).read_bytes()).hexdigest() != original_hash:
                run.record["errors"].append("capture changed during scoring")
    if not run.record["summary"].get("scored_samples"):
        run.record["errors"].append("no samples produced a finite score")
