# Cloudflare Workers AI generator Adapter

Implements streaming answer generation for the Conversation context against
Workers AI's OpenAI-compatible endpoint
(`POST /accounts/{id}/ai/v1/chat/completions`, `stream: true`). It owns the
request and SSE decoding; prompt construction, cancellation, supervision, and
persistence belong to `conversation/chat`.

Credentials come from `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN`
(never from YAML). The token is sent only as a bearer header and is not logged.

Not carried over from Ollama: `think`, `num_ctx` and `keep_alive` have no
Workers AI equivalent. `generator.num_ctx` remains only the chat prompt budget.
Reasoning deltas are ignored; a model that spends its whole output budget on
reasoning returns an explicit generation error rather than an empty answer.
A stream that ends without `[DONE]` or a finish reason is an error.

Change here for provider protocol or stream behavior. Verify with
`go test ./internal/providers/cloudflare/generator`.
