---
title: "Nadir"
description: "Nadir answers questions from private documents using meaning and keyword search."
seoTitle: "Nadir: Answers from Private Documents"
seoDescription: "How Nadir searches documents, writes answers, and checks quality and speed."
answerSummary: "Nadir finds useful text by meaning and by matching words, then uses it to write answers with source references. Ragas checks answer quality, and Locust measures speed."
tags: [system-design, llm, rag]
links:
  github: "https://github.com/Chandra179/nadir"
created: 2026-09-10
updated: 2026-10-07
---

# Nadir: Answers from Private Documents

Nadir answers questions from documents you choose. It finds useful text in
those documents, then asks a language model to write an answer using it.

You can import documents, inspect sources, ask follow-up questions, edit a
conversation and stop an answer. Answers appear as they are written. Documents
and conversations stay local when the storage and models run locally.

## Algorithms and approach

```text
Documents -> Markdown AST -> small pieces -> saved numbers + word counts
                                                       |
Question -> follow-up rewrite -> numbers + word counts -+
                                                       |
                                            Meaning + keyword search
                                                       |
                                            RRF combines rankings
                                                       |
                                            BGE reranking (optional)
                                                       |
                                            Qwen answer + source references
```

### Parse and split documents

Goldmark parses Markdown into an abstract syntax tree (AST): a tree of headings,
lists and text blocks. Recursive splitting breaks that text at paragraph, line,
sentence and word boundaries while keeping headings and source locations.
Short sections can retain surrounding text to keep their meaning.

### Index documents

EmbeddingGemma 300M turns each piece into a list of numbers, called an embedding
or vector, that represents its meaning. Indexing saves these numbers, word
counts, text and source details for searching.

### Rewrite follow-up questions

Qwen 3.5 4B uses recent messages to make follow-ups understandable on their own
before searching (Rewrite-Retrieve-Read). Rules keep the selected subject when
the question says “it” or “the second one.” First questions skip rewriting;
failed rewrites use the original question. The answer still uses the original
question and document text.

### Search by meaning and words

EmbeddingGemma also turns the question into numbers. **Cosine similarity**
compares those numbers with the document numbers. Higher scores suggest a
closer match in meaning.

**TF/IDF keyword search** matches words. TF counts how often a word appears;
IDF gives words found in fewer documents more weight. This helps with exact
names and technical terms.

**Reciprocal Rank Fusion (RRF)** combines the two search rankings using each
result's position in the lists. Text that ranks highly in both gets more weight.

### Rerank the found text

The optional BGE reranker v2-M3 is a **cross-encoder**: it reads each question/text
pair, gives it a match score and reorders the text. This step is currently off;
if it fails, the original search order is kept.

### Generate an answer

Qwen 3.5 4B is instructed to answer using the best text that fits within its input
limit and add numbered source references. Those references keep the text shown
to the model. They help you check an answer but do not guarantee correctness.

### Reuse search results

A cache saves found text and can reuse it for a similar question. Document
updates clear stale results. Quality tests skip this reuse and search again.

## Evaluation with Ragas

Ragas 0.4.3 checks whether answers agree with the source text and expected answers,
and whether search finds useful text. Scores range from 0 to 1; higher is better.

October 6, 2026: Qwen 3.5 4B answered and graded three questions from two documents,
using EmbeddingGemma 300M for embeddings, with reranking off.

| Score | What it measures | Result |
|---|---|---:|
| Faithfulness | Answer claims supported by the text shown to the model | 1.0000 |
| Factual correctness (F1) | Agreement with the expected answer, counting extra and missing facts | 0.6133 |
| Context precision | Useful text appears near the top of the search results | 0.8611 |
| Context recall | How much of the expected answer is supported by the found text | 0.8333 |

Three questions are too few to judge overall quality; the model's grading also
needs checking against human ratings.

## Benchmark with Locust

Locust 2.46.7 measures how quickly search, chat, cache reuse, follow-ups and uploads
finish, and how many complete tasks finish per second.

October 6, 2026: Qwen 3.5 4B with EmbeddingGemma 300M, two documents, one user,
runs requested for 5–12 seconds and a pause of one second between tasks.

| Test | Completed/failed | Tasks/s | Median time (s) | p95 (s) | First text (s) |
|---|---:|---:|---:|---:|---:|
| Search only | 8 / 0 | 0.811 | 0.210 | 0.220 | — |
| Chat | 2 / 0 | 0.120 | 6.20 | 6.20 | 5.8 |
| Mixed search and chat | 4 / 0 | 0.223 | 0.210 / 5.20 | 0.210 / 5.20 | 4.8 |
| Cache reuse | 3 / 0 | 0.613 | 0.490 | 0.560 | — |
| Follow-up | 1 / 0 | 0.071 | 12.0 | 12.0 | 11 |
| Upload | 4 / 0 | 0.507 | 0.470 | 0.570 | — |

Median is the middle time; p95 estimates when 95% of tasks finish; first text is
the median wait for the first generated text (— means it does not apply).
Mixed times list search then chat; its first text time is for chat.
Tasks/s includes pauses and cleanup.

Short runs with one user cannot establish capacity, and these small samples make
p95 unreliable.

## References

- [RRF paper](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf).
- [Qdrant: Combining search methods](https://qdrant.tech/documentation/search/hybrid-queries/).
- [Query rewriting paper](https://arxiv.org/abs/2305.14283).
- [Ragas: Scoring methods](https://docs.ragas.io/en/stable/concepts/metrics/available_metrics/).
- [Locust: Performance testing](https://docs.locust.io/en/stable/what-is-locust.html).
