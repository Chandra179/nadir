.PHONY: run compose compose-rerank test race vet build wasm-build wasm-smoke wasm-check benchmark benchmark-ui eval eval-generate help check mdn generate-mocks

GO_PACKAGES := ./cmd/... ./internal/...
MOCKERY_VERSION ?= v2.53.7
BENCHMARK_PYTHON ?= .local/benchmark/venv/bin/python
EVAL_PYTHON ?= .local/eval/venv/bin/python

run:
	./scripts/local.sh

compose:
	podman compose -f deploy/compose/compose.yaml up -d

compose-rerank:
	RERANKER_ENABLED=true podman compose -f deploy/compose/compose.yaml --profile rerank up -d

test:
	go test -short -count=1 $(GO_PACKAGES)

race:
	go test -short -race -count=1 $(GO_PACKAGES)

vet:
	go vet $(GO_PACKAGES)

build:
	go build ./cmd/api

WASM_DIR := .local/wasm

# Browser runtime: cmd/wasm assembles the retrieval and indexing graph over
# in-memory storage and host-supplied model functions.
wasm-build:
	mkdir -p $(WASM_DIR)
	GOOS=js GOARCH=wasm go build -o $(WASM_DIR)/nadir.wasm ./cmd/wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" $(WASM_DIR)/wasm_exec.js
	mkdir -p web/dashboard/public/nadir
	cp $(WASM_DIR)/nadir.wasm $(WASM_DIR)/wasm_exec.js web/dashboard/public/nadir/

# End-to-end Node smoke test of the built wasm (ingest, query, snapshot, reset).
wasm-smoke: wasm-build
	node cmd/wasm/smoke.mjs $(WASM_DIR)/wasm_exec.js $(WASM_DIR)/nadir.wasm

# Keeps the provider-independent graph buildable and runnable as WASM (needs
# node). The assembly test drives ingest, chunking, embedding, storage and
# search through in-memory providers inside the wasm runtime.
wasm-check: wasm-smoke
	GOOS=js GOARCH=wasm go vet ./internal/core/... ./internal/bootstrap/assembly ./internal/providers/browser ./internal/providers/memory/... ./cmd/wasm
	GOOS=js GOARCH=wasm go test -count=1 -exec="$$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./internal/bootstrap/assembly ./internal/providers/memory/...

benchmark:
	$(BENCHMARK_PYTHON) -m benchmark $(ARGS)

benchmark-ui:
	$(BENCHMARK_PYTHON) -m benchmark --ui $(ARGS)

eval:
	$(EVAL_PYTHON) -m eval run $(ARGS)

eval-generate:
	$(EVAL_PYTHON) -m eval generate $(ARGS)

help:
	@printf '%s\n' 'make eval-generate ARGS="--documents DIR --generator-base-url URL --generator-model MODEL --embedding-base-url URL --embedding-model MODEL"'
	@printf '%s\n' 'make eval ARGS="--host URL --dataset FILE --judge-base-url URL --judge-model MODEL"'
	@printf '%s\n' 'make benchmark ARGS="--host URL --query QUESTION" (headless); make benchmark-ui ARGS="..." (UI)'
	@printf '%s\n' 'make run | compose | compose-rerank | test | race | vet | build | check | generate-mocks'

check: test vet build

mdn:
	go fix $(GO_PACKAGES)

generate-mocks:
	GOFLAGS=-mod=mod go run github.com/vektra/mockery/v2@$(MOCKERY_VERSION)
