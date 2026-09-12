package rewriter

import retrievalrewriting "nadir/internal/retrieval/rewriting"

// Turn aliases the Retrieval rewriting input for adapter-local tests and
// keeps the adapter implementation focused on the Ollama wire protocol.
type Turn = retrievalrewriting.Turn
