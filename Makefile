.PHONY: vendor up up-prod run sm ingest search generate d test test-short eval-fresh eval-llm eval-hyde splade splade-install reranker docling docling-install marker marker-install snapshot backup dev prod

dev:
	./scripts/dev-local.sh

prod:
	./scripts/prod-start.sh

vendor:
	go mod tidy && go mod vendor

up:
	docker compose up -d

up-prod:
	docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

snapshot:
	./scripts/snapshot-qdrant.sh

backup:
	./scripts/backup-qdrant.sh

run:
	go run ./cmd/http

sm:
	git submodule add https://github.com/Chandra179/gitbook gitbook
	git submodule update --init

sm-update:
	git submodule update --remote --merge

ingest:
	curl -X POST localhost:8080/ingest

search:
	curl -X POST localhost:8080/search \
		-H "Content-Type: application/json" \
		-d '{"query":"In Monte Carlo Tree Search, how do we calculate UCB?","top_k":10}'

# generate — search + stream LLM answer. Requires generator.enabled: true in config/config.yaml.
generate:
	curl -X POST localhost:8080/search \
		-H "Content-Type: application/json" \
		-d '{"query":"In Monte Carlo Tree Search, how do we calculate UCB?","top_k":5,"generate":true}' \
		--no-buffer

d:
	curl -X DELETE localhost:6333/collections/pkb_chunks

# splade-install — install Python deps for SPLADE sidecar (one-time)
splade-install:
	pip install fastembed fastapi uvicorn

# splade — run SPLADE sidecar on :5001. Set sparse_scorer.provider: splade in config/config.yaml to activate.
splade:
	FASTEMBED_CACHE_PATH=$$HOME/.cache/fastembed python cmd/splade/main.py

# reranker — run RERANKER sidecar on :5002. Reranker in config/config.yaml to activate.
reranker:
	HF_HOME=$$HOME/.cache/huggingface python cmd/reranker/main.py

# marker-install — install Python deps for Marker PDF converter (one-time)
marker-install:
	pip install -r services/marker/requirements.txt

# marker — convert all PDFs in pdfs/raw → pdfs/converted (one-shot, run before ingest)
# Replaces docling: produces accurate LaTeX math instead of <!-- formula-not-decoded --> placeholders.
marker:
	mkdir -p pdfs/raw pdfs/converted
	python services/marker/main.py --input pdfs/raw --output pdfs/converted

# docling-install — install Python deps for Docling PDF converter (one-time)
docling-install:
	pip install -r services/docling/requirements.txt

# docling — convert all PDFs in pdfs/raw → pdfs/converted (one-shot, run before ingest)
docling:
	mkdir -p pdfs/raw pdfs/converted
	python services/docling/main.py --input pdfs/raw --output pdfs/converted


# =============================================================================
# TESTING
# =============================================================================
#
#   test        Run all unit tests (fast, no external deps).
#
#   test-short  Same as test but skips anything requiring Docker/Ollama.
#
# =============================================================================

test:
	go test ./...

test-short:
	go test -short ./...

# =============================================================================
# EVAL TARGETS
# =============================================================================
#
# Tests in internal/pkb/ — all config from config/config.yaml; env vars override:
#
#   TestSearchEval      Runs retrieval eval across profiles in testdata/eval_profiles.jsonl.
#                       Queries from testdata/eval_queries.jsonl. Reports MRR, HitRate,
#                       NDCG, Precision@K. Appends run to eval_history.jsonl.
#
# Targets:
#   eval-fresh  Self-contained. Spins ephemeral Qdrant, re-ingests fresh, runs
#               all profiles (tf + splade). Only accurate scorer comparison.
#               Slow (~5-10 min). Pulls qdrant/qdrant Docker image on first run.
#
#   eval-llm    LLM judges relevance live. No qrels needed. Slow + costs tokens.
#               Prereq: make up && make ingest
#
# Environment variables (all optional — defaults from config/config.yaml):
#   EVAL_STORE          live (default) | container
#                         live      = connect to running Qdrant; skip ingest
#                         container = spin ephemeral Qdrant; full re-ingest
#   EVAL_JUDGE          qrels (default) | llm
#                         qrels = pre-computed relevance from testdata/qrels.jsonl
#                         llm   = LLM judges each result live (slow, costs tokens)
#   EVAL_QDRANT_ADDR    override qdrant.addr
#   EVAL_QDRANT_COLLECTION  override qdrant.collection
#   EVAL_LLM_BASE_URL   override eval.llm_base_url
#   EVAL_LLM_MODEL      override eval.llm_model
#   EVAL_LLM_API_KEY    LLM API key (no config.yaml equivalent)
#   EVAL_QRELS_PATH     override qrels file (default: testdata/qrels.jsonl)
#   OLLAMA_ADDR         override embedder.ollama_addr
#
# Compare results across runs:
#   cat eval_history.jsonl | jq '{profile,mrr,hit_rate,ndcg,precision}'
# =============================================================================

# eval-fresh — ephemeral Qdrant container, full re-ingest, all profiles, qrels judge.
# Self-contained. No prereqs. Pulls qdrant/qdrant Docker image on first run.
eval-fresh:
	go run ./cmd/gen-qrels
	EVAL_STORE=container go test -v -timeout 600s -run TestSearchEval ./internal/pkb/

# eval-llm — LLM judges relevance live. No qrels needed. Slow + costs tokens.
# Prereq: make up && make ingest
eval-llm:
	EVAL_STORE=live EVAL_JUDGE=llm go test -v -timeout 600s -run TestSearchEval ./internal/pkb/

# eval-hyde — run only the HyDE profile against live Qdrant + Ollama.
# Prereq: make up && make ingest && ollama pull llama3.1:8b-instruct-q4_K_M
eval-hyde:
	EVAL_STORE=live go test -v -timeout 300s -run 'TestSearchEval/tf-recursive256-hyde' ./internal/pkb/