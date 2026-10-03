# Ollama generator Adapter

Implements streaming answer generation for the Conversation context. It owns
the Ollama request and stream decoding; prompt construction, cancellation,
supervision, and persistence belong to `conversation/chat`.

Change here for provider protocol or stream behavior. Verify with
`go test ./internal/providers/ollama/generator`.

Thinking-only Ollama chunks are drained under the configured timeout and are
not emitted as answer text. Content on the terminal chunk is retained. A model
that exhausts its generation budget without producing answer content returns
an explicit generation error rather than a successful empty answer. Reasoning
models must reserve enough output tokens for both reasoning and the answer.
