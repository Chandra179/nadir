.PHONY: run test race vet build check mdn generate-mocks

GO_PACKAGES := ./internal/platform/configuration ./cmd/... ./internal/...
MOCKERY_VERSION ?= v2.53.7

run:
	./scripts/local.sh

test:
	go test -short -count=1 $(GO_PACKAGES)

race:
	go test -short -race -count=1 $(GO_PACKAGES)

vet:
	go vet $(GO_PACKAGES)

build:
	go build ./cmd/api

check: test vet build

mdn:
	go fix ./...

generate-mocks:
	GOFLAGS=-mod=mod go run github.com/vektra/mockery/v2@$(MOCKERY_VERSION)
