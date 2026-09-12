# Cross-service E2E tests

The dashboard's deterministic browser tests live in `web/dashboard/e2e` so
they run with the dashboard's Playwright dependency. This directory is for
future dependency-backed flows that start the complete Nadir deployment and
exercise Qdrant, Ollama, reranking, and Docling together.
