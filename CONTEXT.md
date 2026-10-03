# Nadir Search Context

Nadir indexes local documents into a searchable corpus and answers chat turns
from retrieved context. This glossary keeps the domain language stable while
the indexing, retrieval, and chat Modules evolve.

## Language

**Document**:
A source file normalized to Markdown before it enters the indexing path.
_Avoid_: blob, attachment

**Document intake**:
The step that accepts a source file and converts supported formats into a
Document.
_Avoid_: upload processing

**Indexing pass**:
The ordered work that deduplicates, chunks, enriches, embeds, and replaces a
Document's points in the corpus.
_Avoid_: import job

**Retrieval**:
The query-time work that embeds a question, searches the corpus, applies the
semantic cache and reranker, and returns ranked chunks.
_Avoid_: database search

**Chat turn**:
One user question together with its retrieval trace and optional generated
answer.
_Avoid_: request, message

**Session**:
An ordered conversation containing Chat turns. Normal turns append; an edit
can replace a turn and its later tail in place.
_Avoid_: thread, chat request

**Source identity**:
The stable path and content hash used to deduplicate a Document and identify
its indexed chunks.
_Avoid_: filename only

**Source version**:
The content SHA-256 for a source file at indexing time. A changed source version
produces new deterministic chunk IDs; publication activates the new version
before retiring the old one.
_Avoid_: latest file without a hash

**Citation**:
A numbered snapshot of evidence actually admitted to an answer prompt, with
its source path, version, section and position. A mapped number identifies
evidence; it does not by itself prove that the answer's claim is supported.
_Avoid_: related search result

**Selected subject**:
A source path and section explicitly selected in the conversation, persisted
to resolve later references. It supplies no factual evidence and does not
exclude contrasting sources. An explicit topic change clears the selection;
ambiguous comparisons do not select one alternative.
_Avoid_: inferred fact, retrieval filter

## Relationships

- **Document intake** produces a **Document**.
- An **Indexing pass** replaces all indexed chunks for one **Document**.
- **Retrieval** reads indexed **Documents** and returns ranked chunks.
- A **Session** contains an ordered sequence of **Chat turns**.
- A **Chat turn** uses **Retrieval** and may start answer generation.
- A **Citation** snapshots admitted evidence from a **Source version**.
- A **Chat turn** may preserve a **Selected subject** for later references.

## Example dialogue

> **Dev:** "Does a PDF become a separate retrieval path?"
> **Domain expert:** "No. Document intake converts it to Markdown, then the
> same indexing pass and retrieval rules apply; its source identity remains the
> original PDF path."

## Flagged ambiguities

- "File" can mean the uploaded bytes or the normalized Document. Use
  **source file** for the bytes and **Document** after intake.
