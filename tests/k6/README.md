# k6 Performance Tests

Full documentation → `docs/OVERVIEW.md` → **k6 Load Tests** section.

## Quick Start

```bash
# Install k6
snap install k6        # Linux
brew install k6        # macOS

# Services must be running
docker compose up -d
ollama serve
docker compose up -d  # includes reranker + splade sidecars

# Run
k6 run tests/k6/smoke.js
BASE_URL=http://localhost:8080 k6 run tests/k6/load.js
k6 run --out json=results.json tests/k6/cache_hit_rate.js
```

## Test Data

Queries in `testdata/queries.json`. Update when KB content changes — domain-specific queries produce real hits and accurate latency numbers.