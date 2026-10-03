# RAG failure research for local P1

Researched 2026-10-03. Scope: personal/local Markdown app, current hardware,
installed Gemma generator/rewriter and EmbeddingGemma. This is a literature and
source-code review, not a new application experiment or acceptance result.

## Finding

Nadir's failures belong to well-studied RAG problems. Relevant retrieval does
not establish evidence sufficiency, correct use of evidence, complete answers,
or correct attribution. Published methods improve particular benchmarks; none
proves that Nadir's installed 4B model will satisfy every P1 requirement.

The [pre-fix bounded local review](../test/evaluation/reports/p1-default-focused-review-20261003.json)
records six supported answers out of nine, no operational failures, and three
material semantic failures. It is historical diagnosis; the subsequent
[accepted P1 evidence](p1-evidence.md) records the fixes and current results.
The source evidence differentiates the original failures:

- Manual acknowledgement is selected in the prior answer, and its section is
  retrieved, but the follow-up answers using Auto's warning. The requested
  automatic crash-recovery sequence is absent from the Manual notes.
- Secant is resolved correctly, but the derivative claim cites its introduction
  instead of its supporting properties section.
- The lock follow-up loses conversation context, retrieves unrelated token
  passages, and omits the expired-worker/new-owner explanation even though a
  supporting passage is admitted.

## What research and open source do

### Resolve conversational references before retrieval

[CONQRR (EMNLP 2022)](https://aclanthology.org/2022.emnlp-main.679/)
rewrites context-dependent questions into standalone queries and trains the
rewriter against retrieval performance. It addresses the fact that a follow-up
cannot always be interpreted from its words alone.

[LangChain's history-aware retriever source](https://github.com/langchain-ai/langchain/blob/master/libs/langchain/langchain_classic/chains/history_aware_retriever.py)
passes raw input to retrieval without history; with history, it uses an LLM
rewrite first. [Haystack's conversational RAG tutorial](https://haystack.deepset.ai/tutorials/48_conversational_rag)
combines stored messages with the current message so the agent can use
conversation context when retrieving.

**Application to Nadir — engineering inference:** preserve an explicitly
resolved subject and selected alternative through both retrieval and generation,
with provenance back to the relevant prior turn. Keep the original current
question authoritative. Test topic switches, failed/unchanged rewrites, edits,
and ambiguous references. A prior generated answer identifies a reference; it
does not establish a new fact. Nadir already has history and rewriting: repair
those boundaries rather than introduce another framework. A correct rewrite
alone cannot fix a generator that chooses the wrong admitted mode section.

### Separate answerability from relevance

[Sufficient Context (ICLR 2025; arXiv v3)](https://arxiv.org/html/2411.06037v3)
distinguishes insufficient evidence from a model failing to use sufficient
evidence. Its selective generation method combines a sufficiency assessment
with confidence to trade answer coverage for higher accuracy among answers.
It uses additional rating/model signals; this is not proof that a similarity
threshold or one self-check prompt solves answerability. Its open-domain
evaluation also permits correct answers from model knowledge; Nadir's
document-only contract is stricter. The [authors' repository](https://github.com/hljoren/sufficientcontext)
publishes findings and autorater resources.

**Application to Nadir — engineering inference:** review sufficiency for the
resolved subject and every requested condition. A section about Manual
acknowledgement does not supply a crash-recovery sequence merely because it is
topically relevant. State the missing detail; do not substitute Auto's behavior
or add a recovery guarantee. Measure false declines alongside hallucinations.
General semantic sufficiency remains model-dependent and must be validated;
keyword overlap and exact source-span matching cannot prove it.

### Evaluate citation support at the claim level

[ALCE (EMNLP 2023)](https://aclanthology.org/2023.emnlp-main.398.pdf)
evaluates answer correctness separately from citation support and relevance.
Its experiments find that post-hoc citation attachment can leave correct prose
with poor attribution; reranking multiple generated candidates improves
citation quality but adds generation work. The [evaluation code](https://github.com/princeton-nlp/ALCE/blob/main/eval.py)
checks sentences against cited passages with an entailment model, rather than
merely testing whether citation numbers exist. Running that implementation
would load a separate model; borrow the assessment design without installing
it here.

[LlamaIndex's CitationQueryEngine source](https://github.com/run-llama/llama_index/blob/main/llama-index-core/llama_index/core/query_engine/citation_query_engine.py)
splits retrieved nodes into numbered citation sources and supplies citation
instructions to the response synthesizer. [Haystack AnswerBuilder](https://docs.haystack.deepset.ai/docs/answerbuilder)
parses reference markers into linked documents. From these implementations,
source mapping is distinct from a semantic support guarantee.

**Application to Nadir — engineering inference:** retain source path, version,
section, anchor and exact admitted text; assess each material claim against
its own cited passage. Keep conditions and negation in that assessment.
Existing unique-verbatim correction is a limited aid. It cannot validate
paraphrases, fix wrong subjects, or recover an omitted explanation. If a future
answer format includes evidence spans, code can validate IDs and spans, but
their semantic sufficiency and completeness still require review.

### Control distracting context without removing necessary evidence

[Lost in the Middle (TACL 2024)](https://arxiv.org/html/2307.03172v3)
shows sensitivity to where relevant evidence appears in longer contexts.
Adding more text is therefore not a universal remedy. These experiments use
different models and context lengths; they suggest a diagnostic, not an
established cause of Nadir's failures.

[Haystack SentenceWindowRetriever](https://docs.haystack.deepset.ai/docs/sentencewindowretriever)
retrieves small units and adds their neighboring context via document metadata.
This separates retrieval granularity from the evidence supplied for answering.

**Application to Nadir — engineering inference:** retain the existing bounded
source windows and section identities. Use a diagnostic replay with supporting
evidence alone, then the actual retrieved set, to distinguish generation failure
from distractor sensitivity. Oracle evidence is diagnostic only, never a
production selector or acceptance result. Do not blindly delete competing
sections: contrasts, limitations and requested alternatives may need them.

### Measure separate failure classes

[RGB (AAAI 2024)](https://arxiv.org/abs/2309.01431)
tests noise robustness, rejection of unanswerable questions, integration of
multiple sources, and counterfactual robustness separately. [Ragas faithfulness](https://docs.ragas.io/en/stable/concepts/metrics/available_metrics/faithfulness/)
checks claims against retrieved context. By its definition, that score alone
does not establish that every requested alternative was answered or that each
inline citation supports its claim.

**Application to Nadir — engineering inference:** retain the unchanged packs;
record subject preservation, evidence sufficiency, factual support, citation
support, completeness, and false declines separately. Direct review remains
necessary while the judge is uncalibrated. Counts of retrieved or mapped sources
are operational checks, not semantic acceptance.

## Methods that do not fit the next bounded fix

[Self-RAG (ICLR 2024)](https://arxiv.org/abs/2310.11511)
trains a model to generate reflection tokens and control retrieval/generation.
It is not equivalent to asking the current generator to approve its own draft.
[CRAG](https://arxiv.org/abs/2401.15884)
uses a retrieval evaluator to select corrective actions, including web search
and evidence decomposition. Those components introduce additional work and,
for web fallback, a different source contract.

Nadir's previous stronger-prompt, quote-planning, scope-filter, critic and model
probes failed to establish a general solution. They remain diagnostic evidence
in the reports; these papers do not justify repeating them without a new,
isolated hypothesis. No new model downloads, fine-tuning, online fallback,
framework migration or open-ended agent loop is proposed.

## Next P1 work

This sequence is a proposed application of the research, not a verified fix:

1. Reproduce Manual-to-Auto using the saved trace. Check resolution and
   generation independently: the former must retain Manual; the latter must
   use Manual evidence and acknowledge absent crash details. Preserve explicit
   topic switches and existing edit/history behavior.
2. Reproduce the Secant citation and lock explanation failures. Check the
   actual cited span and the requested reason/conditions. Reuse existing prompt,
   source-window, citation and evaluation seams.
3. Address expressly requested alternatives and restrictions using the same
   separate completeness and support checks.
4. Run a bounded app check with the installed defaults before unchanged full
   packs, fragile repetitions and retrieval regression gates. A unit test of
   state propagation cannot establish answer quality.
5. Migrate an owner index only after the existing acceptance bars pass.

Keep the 27/30 supported, 5/5 declines, 5/5 follow-ups and no-new-material-failure
bar in TODO.md. Record repeated live outcomes, not just a favorable sample.
If failures persist even with correctly resolved questions and isolated
sufficient evidence, report that measured generator limitation; do not quietly
weaken acceptance or download another model.
