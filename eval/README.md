# Ragas quality evaluation

Prepare questions through **generate → review**, then evaluate a running Nadir
API through **collect → score → inspect**. Python 3.12 and
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
synthetic inputs exist only inside tests. Generated references are drafts until
reviewed against the source documents; they do not establish correctness by themselves.

## Generate and review a draft

[Ragas testset generation](https://docs.ragas.io/en/stable/getstarted/rag_testset_generation/)
uses its native transformations, knowledge graph, personas and eligible query
synthesizers. It contacts the explicitly configured model services, without
contacting Nadir or ingesting documents. The supplied Markdown corpus is in
[`samples/`](samples/); its contents are source inputs, not reviewed answers.

```bash
make eval-generate ARGS="--documents eval/samples --generator-base-url http://127.0.0.1:11435/v1 --generator-model phi4-mini:latest --generator-temperature 0.7 --generator-top-p 0.8 --embedding-base-url http://127.0.0.1:11435/v1 --embedding-model embeddinggemma-300m-q8:latest --ollama-gpu-only"

# Hosted-compatible services: separate explicit models and environment credentials
.local/eval/venv/bin/python -m eval generate \
  --documents /absolute/path/documents \
  --generator-base-url https://your-chat-service.example/v1 \
  --generator-model your-generator --generator-api-key-env EVAL_GENERATOR_KEY \
  --embedding-base-url https://your-embedding-service.example/v1 \
  --embedding-model your-embedder --embedding-api-key-env EVAL_EMBEDDING_KEY
```

Both endpoint/model pairs are required. Credentials never inherit the judge's
configuration. Only credential environment variable names enter reports. Defaults
are `--size 40 --seed 42 --timeout 300 --run-timeout 7200`, with one worker.
`--workers` changes native Ragas concurrency; the default remains one.
`--generator-max-output-tokens` defaults to 4,096 for complete structured
generation responses; it configures the native Ragas model factory. Increase
it deliberately if a response is truncated, within the model's context budget.
`--generator-temperature` optionally sets the model's sampling temperature;
omitting it preserves the Ragas factory default. For Qwen, explicitly
select temperature 0.7 and `--generator-top-p 0.8`, following its
[non-thinking model guidance](https://huggingface.co/Qwen/Qwen3.5-4B/blob/main/README.md).
These settings reduced repetitive responses in local checks, but the installed
Qwen model still failed a native entity-extraction prompt. Phi passed that prompt
and a three-question pilot on October 7; the example uses the explicitly selected
Phi model. These are model inference settings, not custom prompts.
Omitting `--generator-top-p` preserves Ragas's default of 0.1, which produced
repetitive lists and output-budget failures with the installed Qwen model.
`--generator-reasoning-effort` forwards the explicit effort to the model factory;
use `none` for the selected Ollama Qwen model. Seeded sampling does not guarantee
identical responses across model services or framework execution order.

Markdown is discovered recursively in sorted order and snapshotted with SHA-256
hashes. Empty corpora, blank files and invalid UTF-8 fail before model requests.
Pinned `langchain-text-splitters` 1.1.3 prepares chunks of up to 1,000
`cl100k_base` tokens with a 100-token overlap budget and source metadata.
Ragas's supported `generate_with_chunks` pipeline owns all later processing.
Chunk content hashes link generated passages to their source metadata even when
the splitter normalizes whitespace.
Token counts use this splitter encoding, not a promise about the model tokenizer.
On first use, tiktoken downloads its standard encoding into its cache.

Ragas 0.4.3 distributes requests equally over eligible single-hop specific,
multi-hop abstract and multi-hop specific synthesizers. Ineligible types are
omitted by the framework. Native rounding may produce 42 samples for a target of
40. Framework prompts and algorithms remain unchanged; a service must provide
enough context for its full structured prompts and configured output budget.
For local generation, start a temporary Ollama service with
8K context, Flash Attention, a quantized cache and one parallel request. For
Linux models already installed by the system service:

```bash
OLLAMA_HOST=127.0.0.1:11435 \
OLLAMA_MODELS=/usr/share/ollama/.ollama/models \
OLLAMA_CONTEXT_LENGTH=8192 OLLAMA_FLASH_ATTENTION=1 \
OLLAMA_KV_CACHE_TYPE=q8_0 OLLAMA_NUM_PARALLEL=1 ollama serve
```

Point both generation roles at `http://127.0.0.1:11435/v1` and stop this
temporary service afterward. Use the actual installed model directory on other
systems. The
OpenAI-compatible request does not set Ollama's context size. Adjust context
only after checking that complete native prompts and responses fit.

For GPU-only local inference, add `--ollama-gpu-only`. The evaluator preloads a
missing model with an empty native request and checks `/api/ps` before and after
each inference request. Every resident model must report its full allocation in
VRAM; missing models, unknown placement or CPU fallback fail the run. Placement,
model digests, actual contexts, request counts and timings enter provenance.
On a 6 GB GPU, Qwen and embeddings may need to run in separate stages, with
Ollama unloading one to load the other. Python and report processing still use
CPU. Increase parallelism only after verifying full GPU placement and measuring
completed-request throughput; more parallel requests require more memory.
Ollama 0.18.2 explicitly forces one slot for the installed `qwen35` backend,
even with `OLLAMA_NUM_PARALLEL=2`. Check runner logs for the effective setting;
the environment variable alone does not establish concurrent inference.

`--cache-dir /absolute/path/cache` optionally enables Ragas's native disk cache
for successful model responses. This avoids repeating completed preparation
after a failed attempt. Separate namespaces cover endpoint/model, role, seed and
sampling/output settings. Use a new cache directory when the model revision,
server context or credential scope changes. Caching is disabled by default and recorded in
provenance when enabled. Cached responses do not measure fresh inference time.

Generation creates a new ignored `.local/evaluation/<UTC-timestamp>-<run-id>/`;
`--output-dir` requires a new directory. Each draft contains:

- `testset.json`: complete native Ragas samples, supporting reference contexts
  and synthesizer names. Generated wording and framework hop labels are preserved.
- `questions.json`: evaluator inputs with unique IDs, `user_input` and `reference`.
- `report.json`: the shared version-1 envelope with phase `generate`,
  `review_status: unreviewed`, requested/actual counts, source and artifact hashes,
  model settings, eligible distribution, document coverage and review flags.

Native sample callbacks checkpoint completed questions. A supervised process
enforces the overall deadline and stops outstanding generation when cancelled.
Failures and deadlines exit nonzero; interruption exits 130. Partial artifacts
and a failed/interrupted report are retained. An empty partial `questions.json`
is evidence of failure and is not a valid evaluation dataset.

Inspect duplicate questions, uncovered documents and unmapped evidence in the
report. Coverage records which source texts supplied reference passages; it does
not establish topic coverage or answer quality. Review each question and reference
against its passages and source file. Remove ambiguous or irrelevant questions,
correct unsupported claims, and add missing topics or separately reviewed
abstention examples. Select/edit accepted rows into a **separate** reviewed
`questions.json`, preserving the draft and IDs for traceability. Record review
decisions alongside that dataset, then check it before collection:

```bash
.local/eval/venv/bin/python -m eval validate --dataset /absolute/path/reviewed/questions.json
```

Only reviewed questions should establish Nadir's quality baseline. Generation
does not score Nadir or prove the reliability of the selected model's references.

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
make eval ARGS="--host http://127.0.0.1:8200 --dataset /absolute/path/questions.json --judge-base-url http://127.0.0.1:11434/v1 --judge-model qwen3.5:4b --judge-reasoning-effort none"

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

`--judge-reasoning-effort` explicitly forwards the chosen endpoint's supported
effort to Ragas's model factory. Omit it to retain the endpoint's default. For
Ollama Qwen, the example selects `none` to keep the framework's bounded output
available for structured judgments; default thinking can exhaust that budget
before returning JSON. This option changes model inference settings, preserving
Ragas prompts and metric algorithms. Ollama documents the compatibility mapping
in its [OpenAI-compatible API](https://docs.ollama.com/api/openai-compatibility).

Defaults: top-k 5, one repetition, sequential samples and metrics, 120 seconds
per complete API workflow and 300 seconds per metric. Override with `--top-k`,
`--repetitions`, `--timeout` and `--metric-timeout`. Every repetition collects a
fresh answer with semantic cache bypassed. Scores alone do not control exit status.

Collection checks readiness and captures corpus inventory and available model
metadata. Immediate answers and SSE answers are supported. HTTP/API errors,
generation errors, replay gaps, missing completion and timeouts fail the run.
Unfinished turns are cancelled and only sessions minted by collection are deleted.
Cleanup failures are recorded. Existing user sessions and documents remain.

## Retrieval scoring (no generation, no judge)

`python -m eval retrieval` sends each question in `eval/golden/retrieval.json`
to `POST /api/v1/turns` with `generate: false` and `skip_cache: true`, then
checks whether the returned chunks hold the passages the golden set expects.
It needs only Qdrant, the embedder and the API; there is no answer model and no
judge, so a run takes seconds and is repeatable. Use it to compare retrieval
settings (the per-file cap, overfetch, fusion, reranking) before spending time
on Ragas.

```bash
# Check labels against eval/samples; no API needed
.local/eval/venv/bin/python -m eval retrieval --validate

# Score a running API whose corpus is the files in eval/samples
.local/eval/venv/bin/python -m eval retrieval --host http://127.0.0.1:8200
```

**Golden set format.** Each item is `{id, kind, question, relevant, reviewed}`
plus an optional `distractor_files`. A `relevant` entry is a file name and a
verbatim `snippet` from that file; a returned chunk is a hit when it comes from
that file and contains the snippet (insensitive to whitespace, backticks and `*`, because
the index stores Markdown-stripped text; other syntax such as links or tables is not
stripped, so avoid it in snippets). Labels never name
chunk IDs, so changing the chunker does not invalidate them. `--validate` fails
if a snippet is missing or occurs more than once in its file, so keep snippets
short and distinctive and inside a single paragraph; a snippet that crosses a
chunk boundary can never match one chunk.

| `kind` | What it tests |
| --- | --- |
| `fact` | One passage answers the question |
| `multi_chunk_same_file` | Several passages of one file are all needed; stresses the per-file cap through Recall@k |
| `distractor` | A similar passage in `distractor_files` could outrank the right one |
| `multi_file` | Evidence in several files; any one counts for Hit@k, Recall@k counts all |
| `unanswerable` | The corpus has no answer; only the top score is reported, no hit or miss |

**Reading the report.** `summary.reviewed` and `summary.overall` hold Hit@1/3/5/10,
Recall@k and MRR over answerable items (a miss scores zero); `by_kind` splits
them. `distractor_first_rate` is how often a distractor outranked the first
correct chunk. `result_shape` (distinct files and maximum chunks per file in a
result set) shows what the per-file cap does. `reviewed: false` items are
drafts: `summary.gate.gate_quality` is true only with at least 30 reviewed
answerable items, otherwise treat the numbers as a smoke check.

**Misses are probed deeper.** A question with an unmatched passage is asked
again with `--probe-top-k` (default 50). `results.questions[].probe.ranks`
shows where the passage appears; `summary.never_retrieved` lists items whose
passage never appears even then. The per-file cap (`search.max_chunks_per_file`)
still applies to the probe and the request cannot change it, so a passage that
ranks below the cap within its file stays hidden at any depth. A
`never_retrieved` item is therefore either a retrieval miss caused by the cap or a
bad label: open the file and the returned chunks before deciding. Items that ask
for more passages from one file than the cap allows (`multi_chunk_same_file` with
four passages and a cap of 3) cannot reach full Recall@k by construction; read
them as a cap measurement, not a ranking one.

Retrieval scoring assumes the indexed corpus is exactly the files in
`eval/samples`; the run stops when a labelled file is not in the corpus
inventory. To compare two settings, run both against the same index and the
same golden-set hash (recorded in `provenance.dataset`).

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

These metrics replace the old handcrafted judge and MRR/nDCG for answer
quality; judge-free retrieval ranking is covered by `python -m eval retrieval`.
Establish a fresh baseline; historical scores are not interchangeable. The [original RAGAs paper](https://aclanthology.org/2024.eacl-demo.16/)
validated specific models and datasets, not this local judge. A successful live
smoke run verifies integration. Judge reliability, correct abstention, citation
attribution and personal usefulness need separately reviewed examples.

### Reading the scores

- **Sample size.** Fewer than about 30 reviewed questions is an integration
  smoke test: one sample moves a mean by several points, and the same questions
  scored differently under different judges in the project's own history.
- **Judge independence.** When the judge model is also the answer model the
  report sets `provenance.judge.is_generator_model` to `true` and the command
  prints a warning. The run still succeeds; treat its scores as relative
  comparisons at most. `null` means the capture did not record the answer model.
- **Terse answers.** `FactualCorrectness` F1 counts claims in the reference that
  the answer omits. Nadir deliberately answers briefly, so write each reference
  as the smallest complete answer to its question, not a paragraph of context;
  extra reference detail lowers the score without any answer error.
- **Self-referential corpora.** Questions about Nadir's own documents are easy
  lookups. Include other documents and multi-section and abstention questions.

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

CI uses deterministic chat and embedding stubs, including actual Ragas generation
and metric integration, process deadlines and partial checkpoints;
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
