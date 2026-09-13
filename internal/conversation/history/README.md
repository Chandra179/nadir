# Conversation history values

Contains the provider-neutral Session and Turn values used by Chat, HTTP
contracts, and the history persistence Adapter. Mutation policy remains in
`conversation/chat`; storage payloads remain in `adapters/qdrant/history`.

Change here only when the shared history value contract changes. Verify the
Conversation, transport, and history Adapter tests.
