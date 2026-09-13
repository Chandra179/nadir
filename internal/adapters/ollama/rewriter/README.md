# Ollama rewriter Adapter

Implements the Ollama request used by conversational query rewriting. It owns
wire decoding, output cleanup, and timeout behavior; when to rewrite and what
history to provide belong to `retrieval/rewriting` and `conversation/chat`.

Change here for provider protocol behavior. Verify with
`go test ./internal/adapters/ollama/rewriter`.
