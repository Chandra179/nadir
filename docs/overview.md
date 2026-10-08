---
title: "Nadir"
description: "Nadir answers questions from private documents using meaning and keyword search."
seoTitle: "Nadir: Answers from Private Documents"
seoDescription: "How Nadir searches documents, writes answers, and checks quality and speed."
answerSummary: "Nadir finds useful text by meaning and by matching words, then uses it to write answers with source references. Ragas checks answer quality, a retrieval check scores search ranking, and Locust measures speed."
tags: [system-design, llm, rag]
links:
  github: "https://github.com/Chandra179/nadir"
created: 2026-09-10
updated: 2026-10-09
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
                                            Reranker model (optional)
                                                       |
                                            Language model answer + source references
```

### Parse and split documents

Goldmark parses Markdown into an abstract syntax tree (AST): a tree of headings,
lists and text blocks. Recursive splitting breaks that text at paragraph, line,
sentence and word boundaries while keeping headings and source locations.
Short sections can retain surrounding text to keep their meaning.

### Index documents

An embedding model turns each piece into a list of numbers, called an embedding
or vector, that represents its meaning. Indexing saves these numbers, word
counts, text and source details for searching.

### Rewrite follow-up questions

The language model uses recent messages to make follow-ups understandable on their own
before searching (Rewrite-Retrieve-Read). Rules keep the selected subject when
the question says “it” or “the second one.” First questions skip rewriting;
failed rewrites use the original question. The answer still uses the original
question and document text.

### Search by meaning and words

The same embedding model also turns the question into numbers. **Cosine similarity**
compares those numbers with the document numbers. Higher scores suggest a
closer match in meaning.

**TF/IDF keyword search** matches words. TF counts how often a word appears;
IDF gives words found in fewer documents more weight. This helps with exact
names and technical terms.

**Reciprocal Rank Fusion (RRF)** combines the two search rankings using each
result's position in the lists. Text that ranks highly in both gets more weight.

### Rerank the found text

The optional reranker model is a **cross-encoder**: it reads each question/text
pair, gives it a match score and reorders the text. This step is currently off;
if it fails, the original search order is kept.

### Generate an answer

The language model is instructed to answer using the best text that fits within its input
limit and add numbered source references. Those references keep the text shown
to the model. They help you check an answer but do not guarantee correctness.

## Evaluation with Ragas

Ragas checks whether answers agree with the source text and expected answers,
and whether search finds useful text. Scores range from 0 to 1; higher is better.

October 9, 2026: 33 questions with short reference answers, answered by a small
local language model and graded by a different local model, with an embedding
model for search and reranking off.

| Score | What it measures | Result |
|---|---|---:|
| Faithfulness | Answer claims supported by the text shown to the model | 0.74 |
| Factual correctness (F1) | Agreement with the expected answer, counting extra and missing facts | 0.63 |
| Context precision | Useful text appears near the top of the search results | 0.96 |
| Context recall | How much of the expected answer is supported by the found text | 0.90 |

The first two scores understate quality: the grader marked several correct
answers as wrong. Six recall scores could not be computed, and the questions
and reference answers are not yet reviewed by a person. Run time, errors and
next steps are in the [evaluation log](evaluation-log.md).

## Search check with a golden set

A separate check scores search alone, with no answer model and no judge. It
asks 57 questions about 13 documents and counts where the expected passage
appears in the results. **Hit@k** is the share of questions whose passage is in
the top k results. **Recall@k** is the share of expected passages found.
**MRR** (mean reciprocal rank) averages 1 divided by the rank of the first
correct result, so 1.0 means always first. Seven questions have no answer in the
documents and are reported apart. The author drafted the questions and the
reviewer accepted them without edits, so the set is a starting point.

October 8, 2026: 50 answerable questions, top 10 results, at most 3 chunks per
file, with and without fetching 3 times as many candidates before that limit.

| Setting | Hit@1 | Hit@3 | Recall@10 | MRR | Results returned |
|---|---:|---:|---:|---:|---:|
| 3× candidates (current) | 0.78 | 0.94 | 0.81 | 0.847 | 8.6 |
| 1× candidates | 0.78 | 0.94 | 0.81 | 0.843 | 5.2 |
| 3× candidates, 5 chunks per file | 0.78 | 0.94 | 0.86 | 0.852 | 9.4 |

Identical runs vary by about 0.003 MRR, so the extra candidates do not change
ranking quality; they fill the result list. Factual questions reach Hit@3 of
1.00. Questions that need four passages from one file score low (Recall@10 0.43)
because only three chunks per file are allowed. Similar documents, such as two
descriptions of rank fusion, are the other weak spot (Hit@10 0.75). Questions with
no answer still return text with scores up to 0.83, so no score cutoff can
reject them yet.

## Benchmark with Locust

Locust measures how quickly search, chat, cache reuse and follow-ups
finish, and how many complete tasks finish per second.

October 8, 2026: a small local language model and embedding model, 13 documents, 60 seconds
per level after a discarded 20-second warm-up, no failed requests. Cells show
1 / 2 / 4 simultaneous users.

| Test | Median time | p95 | Tasks/s |
|---|---|---|---|
| Search only | 0.10 / 0.11 / 0.13 s | 0.11 / 0.12 / 0.17 s | 0.87 / 1.8 / 3.5 |
| Cache reuse | 0.20 / 0.20 / 0.23 s | 0.22 / 0.23 / 0.27 s | 0.80 / 1.6 / 3.2 |
| Chat | 8.1 / 8.1 / 11 s | 8.1 / 9.5 / 13 s | 0.12 / 0.22 / 0.33 |
| Chat, first text | 6.9 / 6.9 / 9.5 s | 6.9 / 8.3 / 12 s | — |
| Follow-up | 15 / 15 / 17 s | 15 / 15 / 22 s | — |

Median is the middle time; p95 estimates when 95% of tasks finish; first text is
the wait for the first generated text. Search and cache reuse scale evenly to 4
users. Chat does not: four times the users gives 2.75 times the tasks per second,
and the wait for first text rises 38%. About 85% of a chat answer is spent
waiting for the model to start. Uploads were not rerun, and a cold model (about
4.4 seconds to load) and larger document sets are not measured. Short runs with
few users cannot establish capacity.

## Models used

Answers, follow-up rewriting and grading use Qwen 3.5 4B; embeddings use
EmbeddingGemma 300M; the optional reranker is BGE reranker v2-M3 (off by
default). Ragas 0.4.3 and Locust 2.46.7 produced the results above.

## References

- [RRF paper](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf).
- [Qdrant: Combining search methods](https://qdrant.tech/documentation/search/hybrid-queries/).
- [Query rewriting paper](https://arxiv.org/abs/2305.14283).
- [Ragas: Scoring methods](https://docs.ragas.io/en/stable/concepts/metrics/available_metrics/).
- [Locust: Performance testing](https://docs.locust.io/en/stable/what-is-locust.html).
