.PHONY: run compose compose-rerank test race vet build benchmark benchmark-ui eval eval-generate help check mdn generate-mocks

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
