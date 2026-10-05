# API benchmarks with Locust

Use Python **3.12** and Locust **2.46.7** to measure the running Nadir API.
Locust manages users, concurrency, statistics and CSV/HTML reports. The adapter
handles API errors, SSE completion, cache reuse and session cleanup.

## Setup and commands

From the repository root:

```bash
python3.12 -m venv .local/benchmark/venv
.local/benchmark/venv/bin/python -m pip install -r benchmark/requirements.txt
make benchmark ARGS="--host http://127.0.0.1:8100 --query 'What does the indexed document explain?'"
make benchmark-ui ARGS="--host http://127.0.0.1:8100 --query 'What does the indexed document explain?'"
```

The UI listens at `http://127.0.0.1:8089`. Choose the workload and its inputs
before starting users. Stop the run, then exit Locust to save the HTML report.
Metadata records each UI run; native CSV and HTML statistics describe the latest
run. `BENCHMARK_PYTHON` can select another Python environment in the Make commands.

An explicit `--host` is required. The API must pass `/api/v1/ready`. Query
workloads require a nonempty `--query`; follow-up also needs `--follow-up`;
upload needs an existing `--upload-file`. Input validation precedes workload
requests. There is no default corpus or query tied to the removed samples.

Headless defaults: retrieval, **1 user**, **1 user/second** spawn rate, **60s**
run time, **top-k 5**, **120s** per request and complete workflow, and **120s**
graceful stop timeout. Each user waits one second between workflows. Native
Locust flags such as `--users`, `--spawn-rate`, `--run-time`, `--stop-timeout`,
`--csv-full-history` and `--web-port` remain available. Use a stop timeout large
enough for the chosen workflow timeout and cleanup.

```bash
make benchmark ARGS="--host http://127.0.0.1:8100 --workload chat --query 'Explain the indexed design' --users 4 --spawn-rate 2 --run-time 2m"
make benchmark ARGS="--host http://127.0.0.1:8100 --workload mixed --query 'Explain the indexed design' --top-k 5 --timeout 120"
make benchmark ARGS="--host http://127.0.0.1:8100 --workload cache --query 'Explain the indexed design'"
make benchmark ARGS="--host http://127.0.0.1:8100 --workload followup --query 'Explain the indexed design' --follow-up 'What are its limitations?'"
make benchmark ARGS="--host http://127.0.0.1:8200 --workload upload --upload-file /absolute/path/to/evaluation.md"
```

## Workloads

| `--workload` | One workflow | Requirements |
|---|---|---|
| `retrieval` | Retrieval-only turn | Indexed documents matching the query |
| `chat` | Generated answer, immediate or complete SSE stream | Generator enabled |
| `mixed` | Alternating retrieval and chat per user, equal shares over each pair | Generator enabled |
| `cache` | Seed query, then repeat until `from_cache` is observed | Semantic cache enabled; repeat deadline 5s or the workflow timeout, whichever is shorter |
| `followup` | Seed retrieval turn, wait for saved history, then generated follow-up with observed rewriting | History, rewriter and generator enabled; history deadline 15s or the workflow timeout, whichever is shorter |
| `upload` | Multipart upload and fresh indexing completion | Documents intake enabled; PDF inputs additionally need Docling |

Retrieval, chat, mixed and follow-up bypass semantic cache. Cache runs reuse it;
existing cache entries can make the seed a hit too. Follow-up history is polled
before the second turn because persistence can lag the seed response.

Each upload uses a unique filename to prevent unchanged-file skipping. Uploaded
documents **remain in the target index**. Use a separate
evaluation API configured with dedicated document, cache and history collections
and separate source inputs. Do not point upload runs at a reading
index you want to keep stable. Configure these in a copied
[API configuration](../internal/bootstrap/configuration/config.yaml).

## Measurements and reports

Reports default to ignored `.local/benchmark/<UTC timestamp>-<run id>/`:

- `locust_stats.csv`, `locust_stats_history.csv`, `locust_failures.csv`,
  `locust_exceptions.csv`: native Locust CSVs. Final statistics are refreshed
  using Locust's serializer so short runs retain their last samples.
- `locust.html`: native Locust HTML report.
- `metadata.json`: host, workload, supplied queries, upload path/hash/size,
  top-k, timeout, load settings, Git revision, corpus inventory and available
  process metrics before and after the run. Unavailable optional snapshots
  carry an error. Inputs can contain sensitive text; output stays local.

`python -m benchmark --output-dir /absolute/new/run-dir ...` chooses a new output
directory. Existing directories are rejected to preserve earlier runs. Native
`--csv` and `--html` flags can override their report paths.

| Statistics type | Meaning |
|---|---|
| `POST`, `GET`, `DELETE` | Individual HTTP operations, including cleanup and history polling. Stream GET latency includes consumption through the terminal event. |
| `WORKFLOW` | Full retrieval/chat/cache/follow-up/upload attempt, including required polling and all answer tokens; cleanup runs afterward. |
| `TTFT` | Time from the generating POST until the first nonempty token, for successful streamed answers only. Immediate answers have no TTFT sample. |

For throughput, use **`completed_workflows_per_second` in metadata**: successful
`WORKFLOW` completions divided by elapsed run time. Per-workload completed and
failed counts are recorded there too. Native `WORKFLOW` rows provide workflow
latency percentiles; their request counts include failed attempts. Locust's
`Aggregated` row mixes HTTP, workflow and TTFT samples, so it does not represent
completed-workflow throughput. Mixed runs have separate retrieval and chat rows.

HTTP errors, API `error`/`generate_error`, generation `generror`, replay `resync`,
missing stream completion, missing answers, cache misses, absent follow-up
rewriting and timeouts fail the run. Expected 404 history polls during
persistence are accepted. Failed runs exit nonzero; a run with no workflow
attempts also exits nonzero. Valid immediate answers are accepted.

Unfinished turns are cancelled, then only session IDs returned by this run's
new turns are deleted. Cleanup failures appear as failed HTTP operations and
make headless runs fail; cleanup is retried when a user stops. There is no reset
of all sessions or documents.

## Focused verification and retained evidence

```bash
.local/benchmark/venv/bin/python -m unittest discover -s benchmark/tests -p 'test_*.py' -v
```

Tests use a localhost stub API, including delayed history, multiline UTF-8 SSE,
immediate answers, generation failures, timeouts, cache misses, uploads, scoped
cleanup, exit codes and native reports. They require localhost socket access.
CI installs the pinned dependency and runs the same tests.

The legacy API/sidecar benchmarks, saved-answer runner and orphaned importer
test were retired October 5. Their 12 files were backed up and SHA-256 verified
before deletion in ignored
`.local/benchmark-migration/2026-10-05-144042/before-migration.tar.gz`;
`inventory.json` records individual hashes and the archive hash. Restore a
needed member into a temporary directory after checking its inventory entry:

```bash
mkdir -p /tmp/nadir-tool-restore
tar -xzf .local/benchmark-migration/2026-10-05-144042/before-migration.tar.gz \
  -C /tmp/nadir-tool-restore scripts/RETIRED_FILE.py
```

Local backups are not distributed with Git. Historical evidence retains its
original bytes and dated tool identities in the
[report catalog](../test/evaluation/reports/README.md). Retrieval/answer quality
checks use the [Go evaluator](../internal/eval/README.md); manual usefulness
review remains in [TODO](../TODO.md). Local startup and generic judge calibration
tools remain in `scripts/`.
