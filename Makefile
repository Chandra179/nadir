.PHONY: vendor up run ingest search generate reset test test-all \
         eval eval-rag eval-both eval-chunk \
         reranker reranker-install \
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

# test-all — run all tests
test-all:
	go test -count=1 ./...

# eval — run retrieval eval harness over a golden set YAML (requires Qdrant + Ollama running)
# Usage: make eval golden=my-golden.yaml
eval:
	go run ./cmd/eval -golden $(golden) -fetch-k 10 -mode retrieval

# eval-rag — run RAGAS end-to-end eval (requires Qdrant + Ollama LLM for generation + judging)
# Usage: make eval-rag golden=my-golden.yaml
eval-rag:
	go run ./cmd/eval -golden $(golden) -fetch-k 5 -mode rag

# eval-both — run retrieval + RAGAS metrics in one pass
# Usage: make eval-both golden=my-golden.yaml
eval-both:
	go run ./cmd/eval -golden $(golden) -fetch-k 10 -mode both

# eval-chunk — retrieval eval at chunk granularity (paper-comparable; use with --fetch-k >= 10)
# Usage: make eval-chunk golden=my-golden.yaml
eval-chunk:
	go run ./cmd/eval -golden $(golden) -fetch-k 10 -mode retrieval -granularity chunk

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
	curl -X DELETE localhost:6333/collections/documents_chunks

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
