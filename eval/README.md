# Ragas quality evaluation

Evaluate a running Nadir API through capture → score → inspect. Python 3.12 and
Ragas 0.4.3 own the quality metrics. [Locust](../benchmark/README.md) owns load,
workflow throughput and latency. Neither tool is an application runtime dependency.

## Setup and inputs

From the repository root, create a separate environment:

```bash
python3.12 -m venv .local/eval/venv
.local/eval/venv/bin/python -m pip install -r eval/requirements.lock
```

`requirements.in` pins direct dependencies; `requirements.lock` pins the resolved
environment. To update deliberately, run the following and rerun the checks below:

```bash
uv pip compile --python 3.12 eval/requirements.in -o eval/requirements.lock
```
The compatibility pin retains the legacy module imported by Ragas 0.4.3.

Supply a UTF-8 JSON array for the **current indexed corpus**:

```json
[
  {
    "id": "supported-1",
    "user_input": "Which database stores the document chunks?",
    "reference": "Nadir stores document chunks in Qdrant."
  }
]
```

`user_input` and `reference` must be nonempty strings. `id` is optional; supplied
or generated IDs must be unique. Old golden-set schemas are unsupported. Small
synthetic inputs exist only inside tests. Measurement datasets and source documents
are operator inputs, never auto-generated references pretending to be evidence.

Index documents before collection, using the dashboard or document upload API.
Use a separate API instance with separate document/cache/history collections for
experiments. The evaluator does not ingest or reset documents. Compare rerankers
and other configurations by running separately configured API instances against
the same questions and indexed source versions.

## Commands

```bash
# Input checks only; no service requests
.local/eval/venv/bin/python -m eval validate --dataset /absolute/path/questions.json

# Complete local run, explicitly selecting the installed judge
make eval ARGS="--host http://127.0.0.1:8200 --dataset /absolute/path/questions.json --judge-base-url http://127.0.0.1:11434/v1 --judge-model phi4-mini:latest"

# Save answers first; no judge required
.local/eval/venv/bin/python -m eval collect \
  --host http://127.0.0.1:8200 --dataset /absolute/path/questions.json

# Score the saved answers again without contacting Nadir
.local/eval/venv/bin/python -m eval score \
  --capture /absolute/path/run/capture.json \
  --judge-base-url https://your-compatible-service.example/v1 \
  --judge-model your-model --judge-api-key-env EVAL_JUDGE_KEY
```

`python -m eval run` is the full command behind `make eval`. Explicit API host,
dataset and judge endpoint/model are required where applicable. Credentials are
read from the named environment variable; only its name enters reports. Local
services accepting no credentials receive a placeholder key. No endpoint/model
fallback occurs. Only OpenAI-compatible chat-completion endpoints are supported.

Defaults: top-k 5, one repetition, sequential samples and metrics, 120 seconds
per complete API workflow and 300 seconds per metric. Override with `--top-k`,
`--repetitions`, `--timeout` and `--metric-timeout`. Every repetition collects a
fresh answer with semantic cache bypassed. Scores alone do not control exit status.

Collection checks readiness and captures corpus inventory and available model
metadata. Immediate answers and SSE answers are supported. HTTP/API errors,
generation errors, replay gaps, missing completion and timeouts fail the run.
Unfinished turns are cancelled and only sessions minted by collection are deleted.
Cleanup failures are recorded. Existing user sessions and documents remain.

## Metrics and interpretation

| Metric | Ragas input and interpretation |
| --- | --- |
| Faithfulness | Answer claims supported by **admitted citation evidence**, including actual truncation |
| FactualCorrectness (F1) | Claim agreement between generated answer and reference answer |
| ContextPrecision | Average precision of judge relevance verdicts over ranked retrieved chunks |
| ContextRecall | Reference-answer claims attributable to retrieved chunks |

The evaluator invokes the current `ragas.metrics.collections` API and preserves
Ragas prompts and algorithms. No embeddings or custom judge rubric are used.
Empty context inputs are explicitly inapplicable for context-dependent metrics.
Ragas NaN/undefined values become JSON null with a reason; summaries separately
count scored, undefined, failed and pending values. Means include finite scores
only. Failed collection or judge calls exit nonzero, as does a run with no finite
scores. A low finite score does not indicate an execution failure.

These metrics replace the old handcrafted judge and MRR/nDCG. Establish a fresh
baseline; historical scores are not interchangeable. The [original RAGAs paper](https://aclanthology.org/2024.eacl-demo.16/)
validated specific models and datasets, not this local judge. A successful live
smoke run verifies integration. Judge reliability, correct abstention, citation
attribution and personal usefulness need separately reviewed examples.

## Shared result format

Evaluator and Locust use the [version-1 contract](run-report-contract.json).
Standard-library validation and atomic JSON writing live in `report.py`; Locust
imports these helpers without installing or importing Ragas.

New evaluator runs default to ignored
`.local/evaluation/<UTC-timestamp>-<run-id>/`. `--output-dir` selects an explicit
**new** directory; existing directories are rejected.

- `capture.json`: immutable after collection finishes; answers, ranked retrieved
  chunks, admitted evidence, public API snapshots, input SHA-256 and provenance.
- `scores.csv`: one row per sample/metric with value, status and reason.
- `report.json`: version-1 envelope (`tool: evaluator`) with phase, framework
  version, inputs, Git metadata, per-sample scores, coverage, errors and artifact
  hashes. Intermediate/failed/interrupted runs retain partial evidence.

`collect` checkpoints capture samples; `run` saves the capture before scoring.
`score` always creates a new report and records the absolute capture path and
SHA-256. It never calls Nadir or changes the capture. Reports may contain private
source text; retain them according to the corpus owner's requirements. Available
inventory is observational provenance, not proof of all indexed content.

## Verification

```bash
RAGAS_DO_NOT_TRACK=true .local/eval/venv/bin/python -m unittest discover -s eval/tests -p 'test_*.py'
.local/benchmark/venv/bin/python -m unittest discover -s benchmark/tests -p 'test_*.py'
go test -short -count=1 ./cmd/... ./internal/...
git diff --check
```

CI uses deterministic stub responses, including actual Ragas metric integration;
no paid judge or live model is required. `RAGAS_DO_NOT_TRACK=true` disables Ragas
usage telemetry if desired. Live evidence belongs in `.local/`, not committed tests.

## Retired assets and recovery

The Go evaluator and its evaluation-only library were retired on October 6, 2026.
The optional calibration utility and historical datasets/results were also retired.
Historical measurement decisions remain dated in ADRs. Commit `31c84e4` preserves
former tracked tools and results. The SHA-256 verified working-tree migration
backup is under `.local/report-archive/ragas-migration-20261006T134553Z/`, with
`inventory.json`, `RESTORE.md` and `before-change.tar.gz`. Extract only needed
paths into a separate checkout to avoid overwriting current work. Local backups
are ignored and machine-specific; Git is the portable recovery source.
