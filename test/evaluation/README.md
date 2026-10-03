# Evaluation fixtures

The golden retrieval set and committed reports live beside this README. The
development evaluator loads them through `cmd/evaluator` and writes new reports
under this directory unless a different path is supplied.

Generation reports can be produced locally with a separately configured, explicitly acknowledged
judge model. They contain aggregate faithfulness, answer relevancy, context
precision, context recall, coverage, failures, and model-call latency, and persist the generated answers, admitted context, citations and provenance
for review. Per-query diagnostics
include paired Retrieval rank/counts, context size and truncation, answer and
judge status, failure class, and diagnostic cause.

The answer request uses temperature `0` and the configured
`generator.max_output_tokens` as Ollama `num_predict`; the judge uses a
temperature-zero 128-token JSON-schema response. Invalid, incomplete, and
out-of-range judge output remains a contract failure rather than being
silently repaired.

The active `golden.json` is a schema-v3 synthetic candidate pack with 133
queries over the four committed sample documents. It includes canonical
relevance labels, two separately recorded synthetic judgment passes, per-query
adjudication metadata, query-intent tags, and a deterministic corpus manifest.
The annotator passes are not independent human judgments and
`metadata.release_gate` remains `false`; this fixture is for regression and
E2E testing only.

Generate or refresh the candidate metadata with:

```bash
python3 scripts/generate_evaluation_candidate.py
```

Run it against local Qdrant and Ollama with:

```bash
go run ./cmd/evaluator --golden test/evaluation/golden.json \
  --no-rerank --runs 1 --ensure-ingest \
  --report test/evaluation/reports/e2e-generated-golden.json
```

The evaluator's `--require-release-gate` mode requires schema-v3 production
metadata, an approved privacy review, a representative corpus manifest, and
two verified independent human annotators for every query. The repository
cannot create those external approvals.

## Saved daily-use questions

[`daily-use-questions.json`](daily-use-questions.json) contains 30 answerable
questions, 5 unsupported/private/live questions and 5 follow-ups authored by
Codex acting as a general user of the sample notes. It was saved before the
first run and records source hashes and expected evidence. These are simulated
questions, not production user data or independent human judgments.

```bash
python3 scripts/run_daily_use.py --base-url http://127.0.0.1:8100 \
  --report test/evaluation/reports/my-daily-use-run.json
```

The runner creates its own sessions, preserves answers/citations and deletes
only those sessions on completion unless `--keep-sessions` is used. It refuses
to overwrite a report and checks source hashes. Structural citation matches
are diagnostics: valid citation numbers do not prove the cited text supports
a claim. Review each answer against the actual question and sources.
See [the local acceptance record](../../docs/LOCAL_V1.md).

The latest [corrected-prefix daily run](reports/quality-prefix-daily-20261002.json)
completed 40 turns without operational failures. [Direct review](reports/quality-prefix-agent-review-20261002.json)
records **28/30 supported, 5/5 declines and 2/5 follow-ups**, against the fresh
control's 23/30, 5/5 and 4/5. The supported count passes its bar, but local
acceptance and the semantic no-regression guard fail. Wrong cited sections and
selected-mode loss remain. Earlier retained counts belong to another arm.

[`representative-app-questions.json`](representative-app-questions.json) retains
the existing 64 queries/expectations and all 14 sample hashes. Its [latest full app run](reports/quality-prefix-representative-answers-20261002.json)
reviews at **49/56 supported and 7/8 declines**, versus control 47/56 and 5/8.
Valid citation IDs are structural evidence, not these semantic counts.

[Registration](reports/quality-plan-20261002.json) precedes each measurement.
[Golden](reports/quality-prefix-retrieval-20261002.json) and [broader](reports/quality-prefix-representative-retrieval-20261002.json)
retrieval each run three times. Golden Hit/Recall/MRR/nDCG rise from
0.977/0.969/0.817/0.837 to 1.000/0.986/0.842/0.860; distractor hits rise
0.226 → 0.278. [Central-text audit](reports/quality-prefix-central-evidence-audit-20261002.json)
checks captured rankings without expanded-window credit. Retrieval guards pass;
semantic acceptance does not. Three earlier repetitions of the [13 fragile cases](reports/quality-repeat-questions-20261002.json)
reproduce mode failure and variable completeness; all observations remain.

The installed-8B CPU-embedding comparison also completed all 104 questions and
both retrieval packs three times. Its [direct review](reports/quality-cpue-model-agent-review-20261002.json)
is **26/30, 5/5, 3/5** and **49/56, 8/8**; it is rejected as the default model.
Its daily first-token p50/p95 is 1.115/13.097 s including cold-load delays;
subsequent broader p50/p95 is 0.861/1.161 s. The earlier GPU-default 8B run
stopped at 24 cases and is explicitly incomplete.

Optional `embedder.num_gpu` / `EMBEDDER_NUM_GPU` is now wired through the existing
provider. A separate [direct-provider 13-case smoke](reports/quality-cpu-direct-smoke-20261002.json)
uses the new code without the diagnostic proxy; operational checks and model
coexistence pass, but [semantic review](reports/quality-cpu-direct-smoke-review-20261002.json)
is 9/13. This is wiring evidence, not a replacement full acceptance run.
[Residency](reports/quality-cpu-direct-residence-20261002.json) records CPU embedding
and GPU generation together. Defaults remain unset and models unchanged.

Other `quality-*` arms and captured-prompt replays are diagnostic experiments,
not accepted release results. In particular, the first label arm's broader
answer report stopped after ten cases. Earlier `daily-use-after`, `final`,
`verified`, `current`, `shipping` and `finalfix` reports are intermediate runs.
Source scopes and indexing inputs changed, so existing corpora require a full
reindex into fresh document/cache collections; see the [safe migration](../../docs/LOCAL_V1.md#reindexing-the-working-source-policy-safely).

## Optional public-math research

`scripts/import_arqmath.py` can build an ARQMath Task 1 candidate and normalize
the licensed corpus into Markdown. The pack and corpus are not checked in.
This work is outside the personal/local technical-notes release. Independent
human labels and any required dataset permissions must precede using public
research results as a release gate.

Fresh schema-v2 default-path reports were recorded on 2026-09-30; services are
available on the measured laptop. The remaining calibration step is human
review of the 39-case blind packet in `judge-calibration/20260930-phi4-mini/`.
The observed Phi-4-mini judge is 3.8B and remains unreviewed; its aggregate
scores are diagnostics rather than acceptance evidence.
