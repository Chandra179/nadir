package rewriter

import conversationrewriting "nadir/internal/core/conversation/rewriting"

// Turn aliases the Retrieval rewriting input for adapter-local tests and
// keeps the adapter implementation focused on the Ollama wire protocol.
type Turn = conversationrewriting.Turn
