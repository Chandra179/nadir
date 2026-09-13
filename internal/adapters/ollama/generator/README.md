# Ollama generator Adapter

Implements streaming answer generation for the Conversation context. It owns
the Ollama request and stream decoding; prompt construction, cancellation,
supervision, and persistence belong to `conversation/chat`.

Change here for provider protocol or stream behavior. Verify with
`go test ./internal/adapters/ollama/generator`.
