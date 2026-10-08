"""Deterministic retrieval scoring against a reviewed golden set.

No generation and no judge model: each question is sent to the API with
``generate: false`` and every returned chunk is compared with the source
passages the golden set says must come back. Labels anchor on a verbatim
snippet and a file name, never a chunk ID, so re-chunking cannot invalidate
them.
"""

import asyncio
from collections import Counter
import hashlib
import json
from pathlib import Path, PurePosixPath
import re

from eval.api import ApiClient
from eval.report import now

KINDS = ("fact", "multi_chunk_same_file", "distractor", "multi_file", "unanswerable")
CUTOFFS = (1, 3, 5, 10)
MIN_REVIEWED = 30  # below this, a run is a smoke check, not evidence
ROW_FIELDS = {"id", "kind", "question", "relevant", "reviewed", "distractor_files"}


def normalize(text):
    # The indexer stores Markdown-stripped text, so inline code and emphasis
    # markers are dropped on both sides before comparing.
    return re.sub(r"\s+", " ", re.sub(r"[`*]", "", text)).strip()


def load_golden(path):
    path = Path(path).resolve()
    raw = path.read_bytes()
    rows = json.loads(raw.decode("utf-8"))
    if not isinstance(rows, list) or not rows:
        raise ValueError("golden set must be a nonempty JSON array")
    ids = set()
    normalized = []
    for index, row in enumerate(rows):
        label = f"golden item {index}"
        if not isinstance(row, dict) or set(row) - ROW_FIELDS:
            raise ValueError(f"{label}: expected only {sorted(ROW_FIELDS)}")
        identifier = row.get("id")
        if not isinstance(identifier, str) or not identifier.strip() or identifier in ids:
            raise ValueError(f"{label}: id must be a nonempty unique string")
        ids.add(identifier)
        label = f"golden item {identifier}"
        if row.get("kind") not in KINDS:
            raise ValueError(f"{label}: kind must be one of {KINDS}")
        if not isinstance(row.get("question"), str) or not row["question"].strip():
            raise ValueError(f"{label}: question must be a nonempty string")
        if not isinstance(row.get("reviewed"), bool):
            raise ValueError(f"{label}: reviewed must be true or false")
        relevant = row.get("relevant")
        if not isinstance(relevant, list):
            raise ValueError(f"{label}: relevant must be an array")
        for rel in relevant:
            if (not isinstance(rel, dict) or set(rel) != {"file", "snippet"}
                    or not all(isinstance(rel[key], str) and rel[key].strip() for key in rel)):
                raise ValueError(f"{label}: each relevant entry needs a file and a snippet")
        if (row["kind"] == "unanswerable") != (not relevant):
            raise ValueError(f"{label}: only unanswerable items may have an empty relevant list")
        distractors = row.get("distractor_files", [])
        if not isinstance(distractors, list) or not all(isinstance(x, str) and x for x in distractors):
            raise ValueError(f"{label}: distractor_files must be an array of file names")
        normalized.append({**row, "distractor_files": distractors})
    return normalized, {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest(),
                        "bytes": len(raw), "kind": "json"}


def check_sources(rows, samples_dir):
    """Return label problems found by comparing the golden set with source files."""
    documents = {p.name: normalize(p.read_text(encoding="utf-8")) for p in Path(samples_dir).glob("*.md")}
    problems = []
    for row in rows:
        for rel in row["relevant"]:
            text = documents.get(rel["file"])
            if text is None:
                problems.append(f"{row['id']}: unknown file {rel['file']}")
                continue
            count = text.count(normalize(rel["snippet"]))
            if count != 1:
                problems.append(f"{row['id']}: snippet occurs {count} times in {rel['file']}: {rel['snippet'][:60]!r}")
        for name in row["distractor_files"]:
            if name not in documents:
                problems.append(f"{row['id']}: unknown distractor file {name}")
    return problems


def ranked(turn):
    chunks = turn.get("results") or []
    if not isinstance(chunks, list) or not all(isinstance(x, dict) and isinstance(x.get("text"), str)
                                                and isinstance(x.get("file_path"), str) for x in chunks):
        raise ValueError("turn response has invalid results")
    return sorted(chunks, key=lambda x: x.get("retrieval_rank", 0))


def match_rank(results, rel):
    """1-based position of the first chunk that holds the snippet, or None."""
    snippet = normalize(rel["snippet"])
    for position, chunk in enumerate(results, 1):
        if PurePosixPath(chunk["file_path"]).name == rel["file"] and snippet in normalize(chunk["text"]):
            return position
    return None


def score_question(row, results):
    files = [PurePosixPath(x["file_path"]).name for x in results]
    per_file = Counter(files)
    entry = {"id": row["id"], "kind": row["kind"], "reviewed": row["reviewed"], "status": "completed",
             "returned": len(results), "distinct_files": len(per_file),
             "max_chunks_per_file": max(per_file.values(), default=0)}
    if not row["relevant"]:
        entry["top_score"] = results[0].get("score") if results else None
        return entry
    ranks = [match_rank(results, rel) for rel in row["relevant"]]
    found = [rank for rank in ranks if rank is not None]
    first = min(found) if found else None
    entry.update(ranks=ranks, first_rank=first,
                 hit={k: first is not None and first <= k for k in CUTOFFS},
                 recall={k: sum(rank is not None and rank <= k for rank in ranks) / len(ranks) for k in CUTOFFS})
    if row["distractor_files"]:
        distractor = next((i for i, name in enumerate(files, 1) if name in row["distractor_files"]), None)
        entry["distractor_rank"] = distractor
        entry["distractor_first"] = distractor is not None and (first is None or distractor < first)
    return entry


def mean(values):
    values = list(values)
    return sum(values) / len(values) if values else None


def aggregate(entries):
    done = [e for e in entries if e["status"] == "completed"]
    answerable = [e for e in done if "ranks" in e]
    unanswerable = [e for e in done if "ranks" not in e]
    out = {"questions": len(entries), "failed": len(entries) - len(done), "answerable": len(answerable),
           "unanswerable": len(unanswerable)}
    if answerable:
        out["hit_rate"] = {str(k): mean(e["hit"][k] for e in answerable) for k in CUTOFFS}
        out["recall"] = {str(k): mean(e["recall"][k] for e in answerable) for k in CUTOFFS}
        out["mrr"] = mean(1 / e["first_rank"] if e["first_rank"] else 0 for e in answerable)
    distractors = [e for e in answerable if "distractor_first" in e]
    if distractors:
        out["distractor_first_rate"] = mean(e["distractor_first"] for e in distractors)
    if done:
        out["result_shape"] = {"mean_returned": mean(e["returned"] for e in done),
                               "mean_distinct_files": mean(e["distinct_files"] for e in done),
                               "mean_max_chunks_per_file": mean(e["max_chunks_per_file"] for e in done)}
    scores = [e["top_score"] for e in unanswerable if isinstance(e.get("top_score"), (int, float))]
    if scores:
        out["unanswerable_top_score"] = {"mean": mean(scores), "max": max(scores)}
    return out


def summarize(entries, top_k):
    reviewed = [e for e in entries if e["reviewed"]]
    reviewed_answerable = sum("ranks" in e for e in reviewed if e["status"] == "completed")
    return {"top_k": top_k, "overall": aggregate(entries), "reviewed": aggregate(reviewed),
            "by_kind": {kind: aggregate([e for e in entries if e["kind"] == kind]) for kind in KINDS},
            "gate": {"reviewed_answerable": reviewed_answerable, "minimum": MIN_REVIEWED,
                     "gate_quality": reviewed_answerable >= MIN_REVIEWED}}


def inventory_names(provenance):
    documents = (provenance.get("corpus_inventory") or {}).get("documents") or []
    return {PurePosixPath(x["file_path"]).name for x in documents if isinstance(x, dict) and "file_path" in x}


async def collect(run, rows, snapshot, host, top_k, timeout, probe_top_k, *, api_factory=ApiClient):
    api = api_factory(host, timeout)
    entries = run.record["results"]["questions"]
    run.record["provenance"]["dataset"] = snapshot
    try:
        provenance = await api.provenance()
        run.record["provenance"]["api"] = provenance
        missing = sorted({rel["file"] for row in rows for rel in row["relevant"]} - inventory_names(provenance))
        if missing:
            raise ValueError(f"golden files are not in the indexed corpus: {', '.join(missing)}")
        for row in rows:
            try:
                async with asyncio.timeout(timeout):
                    turn = await api.request("POST", "/api/v1/turns", json={
                        "query": row["question"], "top_k": top_k, "generate": False, "skip_cache": True})
                    if turn.get("from_cache"):
                        raise ValueError("API returned a cache hit despite skip_cache")
                    entry = score_question(row, ranked(turn))
                    unfound = [rel for rel, rank in zip(row["relevant"], entry.get("ranks", [])) if rank is None]
                    if unfound and probe_top_k > top_k:
                        # Separates "ranked too low" from "never retrieved", which often
                        # means the label itself is wrong (for example a snippet that
                        # straddles a chunk boundary).
                        deep = ranked(await api.request("POST", "/api/v1/turns", json={
                            "query": row["question"], "top_k": probe_top_k, "generate": False, "skip_cache": True}))
                        entry["probe"] = {"top_k": probe_top_k, "ranks": [match_rank(deep, rel) for rel in row["relevant"]]}
                    entries.append(entry)
            except Exception as error:
                message = f"{row['id']}: {type(error).__name__}: {error}"
                run.record["errors"].append(message)
                entries.append({"id": row["id"], "kind": row["kind"], "reviewed": row["reviewed"],
                                "status": "failed", "error": message})
            run.save()
    finally:
        try:
            run.record["errors"].extend(await api.cleanup())
        finally:
            await api.close()


def finish(run, top_k, interrupted):
    entries = run.record["results"]["questions"]
    run.record["summary"] = summarize(entries, top_k)
    probed = [e for e in entries if "probe" in e]
    run.record["summary"]["never_retrieved"] = sorted(
        e["id"] for e in probed if any(rank is None for rank in e["probe"]["ranks"]))
    run.finish(interrupted)


def run_command(options, run_factory):
    """Entry point for ``python -m eval retrieval``; returns a process exit code."""
    rows, snapshot = load_golden(options.golden)
    problems = check_sources(rows, options.samples_dir)
    reviewed = sum(row["reviewed"] for row in rows)
    if options.validate or problems:
        for problem in problems:
            print(f"label problem: {problem}")
        print(f"Golden set: {len(rows)} items, {reviewed} reviewed, {len(problems)} label problems")
        return 1 if problems else 0
    if not options.host:
        raise ValueError("--host is required unless --validate is given")
    inputs = {k: str(v) if isinstance(v, Path) else v for k, v in vars(options).items() if k != "output_dir"}
    run = run_factory("retrieval", inputs, options.output_dir)
    run.record["provenance"] = {key: run.record["provenance"][key] for key in ("phase", "producer")}
    run.record["provenance"]["scorer"] = {"started_at": now(), "cache": "skipped", "generation": "disabled"}
    run.record["results"] = {"questions": []}
    interrupted = False
    try:
        asyncio.run(collect(run, rows, snapshot, options.host, options.top_k, options.timeout, options.probe_top_k))
    except (asyncio.CancelledError, KeyboardInterrupt):
        interrupted = True
        run.record["errors"].append("retrieval scoring interrupted")
    except Exception as error:
        run.record["errors"].append(f"{type(error).__name__}: {error}")
    finish(run, options.top_k, interrupted)
    summary = run.record["summary"]
    overall = summary["reviewed" if summary["gate"]["reviewed_answerable"] else "overall"]
    print(json.dumps({"mrr": overall.get("mrr"), "hit_rate": overall.get("hit_rate"), "gate": summary["gate"],
                      "errors": len(run.record["errors"])}, indent=2))
    return 130 if interrupted else 1 if run.record["errors"] else 0
