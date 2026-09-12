# Helm deployment

The Helm chart is intentionally deferred with the Kubernetes manifests. A
chart would otherwise make the current single-node Chat event log look like a
safe active-active deployment. Keep the Compose workflow as the supported
portable deployment until the distributed invariants in `docs/SCALING.md` are
implemented and tested.
