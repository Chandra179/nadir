.PHONY: vendor up run sm sm-update ingest search generate reset test test-all \
         eval-fresh eval-llm eval-hyde \
         splade splade-install reranker reranker-install \
         docling docling-install \
         dev check

# check — verify all required tools are installed before running dev
check:
	@command -v docker >/dev/null 2>&1 || (echo "ERROR: docker not found"; exit 1)
	@command -v go >/dev/null 2>&1 || (echo "ERROR: go not found"; exit 1)
	@command -v python3 >/dev/null 2>&1 || (echo "ERROR: python3 not found"; exit 1)
	@curl -sf http://localhost:11434/api/tags >/dev/null 2>&1 || echo "WARN: ollama not running (needed for embeddings)"
	@echo "prereqs OK"

dev:
	./scripts/dev-local.sh

vendor:
	go mod tidy && go mod vendor

up:
	docker compose up -d

run:
	go run ./cmd/server

# test — run unit tests only (no Docker/Qdrant required)
test:
	go test -short -count=1 ./...

# test-all — run all tests including eval (requires Docker for Qdrant testcontainers)
test-all:
	go test -count=1 ./...

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
		-d '{"query":"secant formula","top_k":10}'

# generate — search + stream LLM answer. Requires generator.enabled: true in config/config.yaml.
generate:
	curl -X POST localhost:8080/search \
		-H "Content-Type: application/json" \
		-d '{"query":"cosecant formula","top_k":5,"generate":true}' \
		--no-buffer

reset:
	curl -X DELETE localhost:6333/collections/pkb_chunks

# splade-install — install Python deps for SPLADE sidecar (one-time)
splade-install:
	pip install fastembed fastapi uvicorn

# splade — run SPLADE sidecar on :5001. Set sparse_scorer.provider: splade in config/config.yaml to activate.
splade:
	FASTEMBED_CACHE_PATH=$$HOME/.cache/fastembed python3 services/splade/main.py

# reranker-install — install Python deps for reranker sidecar (one-time)
reranker-install:
	pip install -r services/reranker/requirements.txt

# reranker — run RERANKER sidecar on :5002. Set reranker.enabled: true in config/config.yaml to activate.
reranker:
	HF_HOME=$$HOME/.cache/huggingface python3 services/reranker/main.py

# docling-install — install Python deps for Docling PDF converter (one-time)
docling-install:
	pip install -r services/docling/requirements.txt

# docling — convert all PDFs in pdfs/raw → pdfs/converted (one-shot, run before ingest)
docling:
	mkdir -p pdfs/raw pdfs/converted
	python3 services/docling/main.py --input pdfs/raw --output pdfs/converted

# =============================================================================
# EVAL TARGETS
# =============================================================================
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
