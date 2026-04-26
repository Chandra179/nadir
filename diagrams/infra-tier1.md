# Tier 1 Infrastructure — Single Box

```mermaid
graph TD
    subgraph Client
        U[HTTP Client]
    end

    subgraph "Hetzner CX52 — single box"
        subgraph "Optional TLS proxy"
            N[nginx :443]
        end

        subgraph "Go HTTP Server :8080"
            IH[POST /ingest]
            SH[POST /search]
            HZ[GET /healthz]
        end

        subgraph "Vector DB"
            QD[Qdrant :6333 REST\n:6334 gRPC\ndense + BM25 sparse]
        end

        subgraph "ML sidecars"
            OL[Ollama :11434\nnomic-embed-text\n768-dim dense]
            SP[SPLADE :5001\nnaver/splade-cocondenser\nsparse vectors]
            RR[Reranker :5002\ncross-encoder\nms-marco-MiniLM-L-6-v2]
        end

        subgraph "Observability"
            PR[Prometheus :9090]
            GR[Grafana :3000]
            NE[node-exporter :9100]
        end

        subgraph "Storage"
            QV[(qdrant_data volume)]
            SC[(splade_model_cache volume)]
            HC[(huggingface_cache volume)]
        end
    end

    U -->|HTTPS| N
    N -->|HTTP| IH
    N -->|HTTP| SH
    U -->|HTTP dev| IH
    U -->|HTTP dev| SH

    IH -->|gRPC upsert| QD
    IH -->|embed| OL
    IH -->|sparse embed| SP

    SH -->|embed query| OL
    SH -->|sparse embed| SP
    SH -->|HybridSearch RRF| QD
    SH -->|rerank| RR

    QD --- QV
    SP --- SC
    RR --- HC

    PR -->|scrape :8080/metrics| IH
    PR -->|scrape :6333/metrics| QD
    PR -->|scrape :9100| NE
    GR -->|query| PR
```

**Retrieval flow:** query → dense embed (Ollama) + sparse embed (SPLADE) → RRF fusion (Qdrant) → cross-encoder rerank → top-K

**Ingest flow:** markdown → chunk → dense embed (Ollama) + sparse embed (SPLADE) → upsert (Qdrant, SHA dedup)

**Scale trigger:** CPU >70% sustained or SPLADE p99 >500ms → provision ML machine (Tier 2). See `docs/INFRA_SETUP.md`.
