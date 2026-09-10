.PHONY: run test race vet build check mdn

GO_PACKAGES := ./config ./cmd/... ./internal/...

run:
	./scripts/local.sh

test:
	go test -short -count=1 $(GO_PACKAGES)

race:
	go test -short -race -count=1 $(GO_PACKAGES)

vet:
	go vet $(GO_PACKAGES)

build:
	go build ./cmd/server

check: test vet build

mdn:
	go fix ./...
