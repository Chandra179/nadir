# Chunking

Turns normalized document text into bounded chunks and optional sentence
windows with source locations and headings. It owns boundary policy; indexing
owns embedding, enrichment, deduplication, and publication.

Change here for chunk boundaries or chunk metadata. Verify with
`go test ./internal/core/documents/chunking` and the retrieval evaluator after a
quality-affecting change.

Recursive chunks retain bounded central text for embedding and exact source
anchors. A split section no longer than twice `chunk_size` also retains its
complete normalized section as `WindowText`; longer sections do not expand.
Standalone short bold labels introduce scopes within a Markdown heading, so
an automatic-mode warning does not become unlabeled manual-mode evidence.

`Header` remains the filterable Markdown leaf. `SectionPath` retains parent
headings and a label, if present, for persisted citation context. Indexing uses
the full path only when the same leaf occurs under distinct Markdown parent
paths. A label under one unique heading uses `leaf > label`; broad document
titles do not prefix every precise heading. The policy's dated measurements
and design history are recorded in
[ADR 0035](../../../../docs/adr/0035-source-scopes-and-local-quality-candidate.md).
Use the [evaluator guide](../../../../eval/README.md) for new measurements on the
current corpus.

These source scopes and indexing inputs require a full reindex. Source-SHA
skipping cannot upgrade an unchanged file. Preserve old collections and use a
copied config with new document/cache collection names and all source originals.
