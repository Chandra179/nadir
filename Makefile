.PHONY: vendor up run sm ingest search d eval-fresh eval-llm splade splade-install

vendor:
	go mod tidy && go mod vendor

up:
	docker compose up -d

run:
	bash -c 'source .env && go run ./cmd/http' 

sm:
	git submodule add https://github.com/Chandra179/gitbook gitbook
	git submodule update --init

ingest:
	curl -X POST localhost:8080/ingest

search:
	curl -X POST localhost:8080/search \
		-H "Content-Type: application/json" \
		-d '{"query":"system design","top_k":5}'

d:
	curl -X DELETE localhost:6333/collections/pkb_chunks

# splade-install — install Python deps for SPLADE sidecar (one-time)
splade-install:
	pip install fastembed fastapi uvicorn

# splade — run SPLADE sidecar on :5001. Set sparse_scorer.provider: splade in config/config.yaml to activate.
splade:
	python cmd/splade/main.py


# =============================================================================
# EVAL TARGETS
# =============================================================================
#
#   eval-fresh  Self-contained. Spins ephemeral Qdrant, re-ingests fresh, runs
#               all profiles (tf + splade). Only accurate scorer comparison.
#               Slow (~5-10 min). Pulls qdrant/qdrant Docker image on first run.
#
#   eval-llm    LLM judges relevance live. No qrels needed. Slow + costs tokens.
#               Prereq: make up && make ingest
#
# Environment variables (all optional):
#   EVAL_JUDGE          qrels (default) | llm        — relevance judge
#   EVAL_QDRANT_ADDR    override Qdrant addr (default: from config.yaml)
#   EVAL_QDRANT_COLLECTION  override collection name
#   EVAL_LLM_BASE_URL   LLM judge base URL
#   EVAL_LLM_MODEL      LLM judge model name
#   EVAL_LLM_API_KEY    LLM judge API key
#   EVAL_QRELS_PATH     override qrels file path (default: testdata/qrels.jsonl)
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