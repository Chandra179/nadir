# Readiness policy

Aggregates required dependency probes into one operational report. The
composition root supplies probes; this package does not know Qdrant, Ollama,
or sidecar protocols. Disabled optional dependencies are ready, while a
required dependency with no probe is not ready.

Keep liveness and readiness separate: liveness is the dependency-free health
endpoint, readiness is the startup/load-balancer gate.
