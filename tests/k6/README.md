# k6 Performance Tests

Full documentation → `docs/E2E_DOCS.md` → **k6 Load Tests** section.

## Quick Start

```bash
# Install k6
snap install k6        # Linux
brew install k6        # macOS

# Services must be running
docker compose up -d
ollama serve
docker compose -f docker-compose.sidecars.yml up -d  # reranker + splade

# Run
k6 run tests/k6/smoke.js
BASE_URL=http://localhost:8080 k6 run tests/k6/load.js
k6 run --out json=results.json tests/k6/cache_hit_rate.js
```

## Test Data

Queries in `testdata/queries.json`. Update when KB content changes — domain-specific queries produce real hits and accurate latency numbers.

## Baseline Results (2026-05-02)

| Test | Key Metric | Result |
|------|-----------|--------|
| smoke (1 VU) | p50 / p95 / RPS | 32ms / 386ms / 0.88 RPS |
| load (50 VU) | p50 / p95 / RPS | 34ms / 83ms / 42.6 RPS |
| cache_hit_rate (10 VU) | hit rate | 97.47% |
| cache_hit_rate | cached p95 / uncached p95 | 43ms / 93ms |
| ollama_embed (8 VU) | avg / p95 / RPS | 17.6ms / 24.6ms / 183 RPS |
