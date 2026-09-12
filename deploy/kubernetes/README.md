# Kubernetes deployment

Kubernetes manifests are intentionally deferred until the application has a
shared Chat event backend, distributed Session mutation authority, and an
operational Qdrant topology. The current supported deployment is the portable
Compose stack in `deploy/compose`.

When this directory is implemented, the API, dashboard, model backends, and
Qdrant should be separate workloads with readiness probes, resource budgets,
secret references, and an explicit SSE routing policy.
