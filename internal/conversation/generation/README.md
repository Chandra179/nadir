# Generation contract

Defines the provider-neutral streaming generation events consumed by Chat.
Adapters implement the generator capability; this package owns no HTTP,
Ollama, prompt, or persistence behavior.

Change here when the cross-module generation event contract changes. Verify
the Conversation tests and the generator Adapter tests.
