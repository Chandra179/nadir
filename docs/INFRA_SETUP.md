# Production RAG Infrastructure Setup

Stack: Go HTTP server + Qdrant + Ollama embedder + SPLADE + Reranker + Prometheus/Grafana

---

## Architecture

```
                        ┌─────────────┐
                        │  Load Bal.  │  (Caddy / nginx, optional at scale)
                        └──────┬──────┘
                               │
                        ┌──────▼──────┐
                        │  Go HTTP    │  cmd/http — ingest + search
                        │  Server     │
                        └──┬──────┬───┘
                           │      │
              ┌────────────▼─┐  ┌─▼──────────────┐
              │  Qdrant      │  │  Ollama         │
              │  (gRPC)      │  │  nomic-embed    │
              │  dense+BM25  │  │  (768-dim)      │
              └──────────────┘  └─────────────────┘
                           │
              ┌────────────▼─────────────┐
              │  SPLADE service          │  Python, port 8001
              │  (sparse vectors)        │
              └──────────────────────────┘
                           │
              ┌────────────▼─────────────┐
              │  Reranker service        │  Python, cross-encoder, port 8002
              │  (cross-encoder)         │  +10-25% precision over hybrid
              └──────────────────────────┘
                           │
              ┌────────────▼─────────────┐
              │  Prometheus + Grafana    │  metrics + dashboards
              └──────────────────────────┘
```

**Retrieval flow:** dense (nomic) + sparse (BM25 built-in Qdrant) + SPLADE → RRF fusion → cross-encoder rerank → top-K results

---

## Docker Compose Layout

```
Dockerfile                  # Go HTTP server
services/
  splade/
    Dockerfile              # Python + transformers + naver/splade-cocondenser-ensemble-distil
    requirements.txt
  reranker/
    Dockerfile              # Python + sentence-transformers cross-encoder
    requirements.txt
config/
  prometheus/prometheus.yml
  grafana/                  # provisioned dashboards
docker-compose.yml
docker-compose.prod.yml     # prod overrides (resource limits, restart policies)
```

---

## Service Resource Requirements

| Service | CPU | RAM | GPU | Notes |
|---------|-----|-----|-----|-------|
| Go HTTP | 2 cores | 512MB | None | Stateless, scales horizontally |
| Qdrant | 4 cores | 4-8GB | None | Depends on collection size |
| Ollama (nomic) | 4 cores | 4GB | Optional | CPU fine for embed-only |
| SPLADE | 4 cores | 4GB | Recommended | naver model ~500MB |
| Reranker | 4 cores | 4GB | Recommended | cross-encoder ~500MB |
| Prometheus | 1 core | 1GB | None | |
| Grafana | 1 core | 512MB | None | |
| **Total** | **~20 cores** | **~22GB** | Optional | |

---

## Recommended Server Tiers

### Tier 1 — Dev / Small (<500 concurrent users)

**Hetzner CX52** — single machine
- 16 vCPU, 32GB RAM, 320GB NVMe
- ~€38/month
- All services on one box, Compose sufficient
- No GPU: SPLADE + reranker run CPU-only (slower, still usable)

### Tier 2 — Production (500–5k users)

**Hetzner CCX53** (app) + **Hetzner CCX32** (ML inference)
- App: 32 vCPU, 128GB RAM — €~148/month
- ML: 8 vCPU, 32GB RAM — €~55/month
- **Total: ~€203/month**
- Qdrant + Go server on app machine
- SPLADE + reranker + Ollama on ML machine

### Tier 3 — Scale (5k–50k users)

**Hetzner GEX44** (GPU dedicated) + CCX53 (app)
- GPU box: RTX 3090, 64GB RAM — ~€184/month
- App: CCX53 — ~€148/month
- Monitoring: CX32 — ~€15/month
- **Total: ~€347/month**
- GPU for SPLADE + reranker inference, fast latency
- Consider Qdrant Cloud at this scale for HA (~$100/month)

### Tier 4 — Enterprise (50k+ users)

Move to Kubernetes (K3s self-hosted or managed GKE/EKS):
- Qdrant cluster (3 nodes for HA)
- Horizontal pod autoscaling for Go HTTP
- Separate SPLADE/reranker deployments with GPU node pool
- Estimated: €800–2000/month depending on cloud

---

## Cost Breakdown Summary

| Tier | Users | Stack | Monthly Cost |
|------|-------|-------|-------------|
| Dev | <500 concurrent | Single Hetzner CX52 | ~€38 |
| Prod | 500–5k | 2x Hetzner VPS | ~€203 |
| Scale | 5k–50k | GPU box + app VPS | ~€347 |
| Enterprise | 50k+ | K8s cluster | €800–2000+ |

**vs cloud-managed:**
- AWS g5.xlarge (A10G GPU): ~$700/month just for inference
- Qdrant Cloud (1 node): ~$65/month
- Self-hosted Hetzner = 3–6× cheaper at equivalent specs

---

## Monitoring Stack

Prometheus scrapes:
- Go HTTP server: `/metrics` (OTEL prometheus exporter — already wired)
- Qdrant: built-in metrics endpoint
- Node exporter: CPU/RAM/disk on each host

Grafana dashboards:
- RAG latency (embed + search + rerank per request)
- Qdrant collection stats (vectors, segments)
- SPLADE/reranker inference latency
- Error rates by endpoint

Same machine as app fine until >10 services or compliance requires isolation.

---

## Scaling Guide: Tier 1 → Tier 2+

### When to scale up

| Signal | Action |
|--------|--------|
| CPU >70% sustained | Add ML machine (Tier 2) |
| SPLADE/reranker p99 latency >500ms | Move ML to dedicated box |
| Qdrant RAM >80% | Resize Qdrant machine or add Qdrant Cloud |
| Go HTTP goroutines >10k | Horizontal scale (add second app box + LB) |

### Tier 1 → Tier 2 migration (zero-downtime)

1. **Provision ML machine** (Hetzner CCX32, ~€55/mo)
2. **Move SPLADE + reranker + Ollama** to new box:
   - Update `.env`: `SPLADE_ADDR`, `RERANKER_ADDR`, `OLLAMA_ADDR` → new machine IP
   - Deploy services on new machine via same Compose file (target only those services)
   - Verify health: `curl http://<new-ml-ip>:5001/health`
3. **Restart app** with updated env — no data loss, Qdrant stays on original box
4. **Decommission** ML services from Tier 1 box (free ~12GB RAM, reduce CPU contention)

### Tier 2 → Tier 3 (add GPU)

1. Provision Hetzner GEX44 (GPU dedicated)
2. Move SPLADE + reranker to GPU box — same process as above
3. Update Dockerfiles to use `--gpus all` in compose override
4. Ollama can stay CPU or migrate to GPU box (cut embed latency ~10x)

### Tier 3 → Tier 4 (K8s)

1. Containerize each service (already done via Dockerfiles)
2. Write K8s manifests (Deployment + Service per component)
3. Qdrant: use [qdrant-operator](https://github.com/qdrant/qdrant-operator) for cluster setup
4. Add HPA on Go HTTP deployment (CPU-based autoscale)
5. Use node pool with GPU taint for SPLADE/reranker pods

---

## Production Checklist

### Tier 1 (single box)
- [x] Qdrant data on persistent volume: `volumes: qdrant_data:/qdrant/storage`
- [x] Ollama model pre-pulled in Dockerfile or startup script — baked into `services/splade/Dockerfile` + `services/reranker/Dockerfile` at build time
- [x] SPLADE model cached to volume: `splade_model_cache:/root/.cache/fastembed`
- [x] `.env` file present with all vars set (see `.env.example`)
- [x] `GRAFANA_PASSWORD` changed from default `admin` — `.env.example` updated with warning; set in `.env`
- [x] `restart: unless-stopped` on all services in compose
- [x] Resource limits in `docker-compose.prod.yml` (`cpus`, `memory`) — run with `make up-prod`
- [x] Health checks on Go HTTP, SPLADE, reranker services
- [x] nginx TLS config: `config/nginx/nginx.conf` — edit domain, mount certs to `config/nginx/certs/`, uncomment nginx service in compose
- [x] Qdrant snapshot cron: `0 2 * * * /path/to/nadir/scripts/snapshot-qdrant.sh` (or `make snapshot`)
- [x] Prometheus retention: `--storage.tsdb.retention.time=${PROMETHEUS_RETENTION:-30d}` wired in compose
- [x] Node exporter in compose (`node-exporter` service) + Prometheus scrape job added
- [x] `LOGGER_LEVEL=prod` in env

### Additional for Tier 2+
- [ ] Firewall: ML machine only reachable from app machine (not public)
- [ ] Internal network / private VLAN between machines (Hetzner: use same datacenter)
- [ ] Separate backup target for Qdrant snapshots (S3-compatible: Hetzner Object Storage ~€5/mo)
- [ ] Alertmanager configured (Prometheus → PagerDuty/Slack on error rate spike)
- [ ] Log aggregation (Loki + Grafana, or ship to managed service)

---

## Similar Open-Source Stacks (Reference)

- [Danswer](https://github.com/danswer-ai/danswer) — Go/Python hybrid RAG, Qdrant, self-hosted
- [Khoj](https://github.com/khoj-ai/khoj) — personal AI, Qdrant backend
- [RAGFlow](https://github.com/infiniflow/ragflow) — production RAG, similar hybrid search
- [Cognita](https://github.com/truefoundry/cognita) — modular RAG, supports SPLADE + reranker

Pattern confirmed: separate Python sidecar for heavy ML inference (SPLADE/reranker), Go/Rust for serving, Qdrant for vector store.

---

Sources:
- [RAG Pipelines in Production — DEV Community](https://dev.to/pooyagolchian/rag-pipelines-in-production-vector-database-benchmarks-chunking-strategies-and-hybrid-search-data-gbl)
- [RAG in Production 2026 — Apify](https://use-apify.com/blog/rag-production-architecture-2026)
- [Hetzner Cloud for AI Projects 2026 — DEV Community](https://dev.to/jangwook_kim_e31e7291ad98/hetzner-cloud-for-ai-projects-complete-gpu-server-setup-cost-breakdown-2026-58i4)
- [RAG Infrastructure Guide — Introl](https://introl.com/blog/rag-infrastructure-production-retrieval-augmented-generation-guide)
- [Run Ollama on VPS 2026 — DanubeData](https://danubedata.ro/blog/run-ollama-vps-self-host-llm-2026)
- [RAG Techniques 2026 — Starmorph](https://blog.starmorph.com/blog/rag-techniques-compared-best-practices-guide)
