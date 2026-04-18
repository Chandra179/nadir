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