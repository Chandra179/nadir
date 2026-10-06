# 0036 — Ragas evaluation through the public API

- **Status:** Accepted
- **Date:** 2026-10-06
- **Supersedes:** [0018](0018-repeatable-retrieval-evaluation.md)

## Context

The Go evaluator constructed an application graph and maintained custom ranking
and judge code. It was strongly coupled to internal retrieval/prompt seams.
The user chose a maintained Python quality framework and fresh run artifacts,
after retiring historical evaluation inputs and calibration tools.

The [RAGAs paper](https://aclanthology.org/2024.eacl-demo.16/) supports structured
RAG evaluation, while the [NVIDIA evaluation implementation](https://github.com/NVIDIA-AI-Blueprints/rag/tree/main/scripts/eval)
provides a deployed-API capture-before-scoring pattern. Neither establishes
Phi-4-mini judge reliability for Nadir's corpus.

## Decision

Root `eval/` uses Python 3.12 and Ragas 0.4.3 collections metrics. Capture through
the public API, then score immutable answers with explicit OpenAI-compatible
judge endpoint/model configuration. Use Faithfulness, FactualCorrectness F1,
ContextPrecision and ContextRecall without replacing framework prompts/scoring.
Faithfulness uses admitted citation evidence; retrieval metrics use ranked chunks.

Keep Locust in `benchmark/` for performance. Share only standard-library report
validation/atomic writing and the version-1 envelope. Generated results remain
under ignored `.local/`; current datasets and documents are operator inputs.
Re-scoring references the source capture hash and never calls Nadir.

Remove the Go evaluator, its whole evaluation-only library and exclusive runtime
options/statistics wiring after replacement verification. Preserve application
API behavior, configured retrieval/cache policies, regression checks and consumed
mocks. Recovery uses commit `31c84e4` and a verified local working-tree backup.

## Consequences

- Metrics require a fresh baseline; custom judge scores and MRR/nDCG are retired.
- Ragas owns scoring; the adapter owns API lifecycle and provenance only.
- Empty/undefined inputs retain null scores and coverage reasons. Judge/transport
  errors fail execution; low finite scores do not.
- Local and hosted compatible judges can re-score the same answers. No native
  vendor clients, embedding-dependent metric, ingestion/reset or release gate.
- Integration smoke results verify plumbing. Abstention, citation attribution,
  judge reliability and personal usefulness need reviewed examples separately.
