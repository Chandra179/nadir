.PHONY: run compose compose-rerank test race vet build load-benchmark user-path-benchmark check mdn generate-mocks

GO_PACKAGES := ./cmd/... ./internal/...
MOCKERY_VERSION ?= v2.53.7

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

load-benchmark:
	python3 scripts/benchmark_load.py $(ARGS)

user-path-benchmark:
	python3 scripts/benchmark_user_paths.py $(ARGS)

check: test vet build

mdn:
	go fix ./...

generate-mocks:
	GOFLAGS=-mod=mod go run github.com/vektra/mockery/v2@$(MOCKERY_VERSION)
