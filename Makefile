.PHONY: vendor up run sm ingest search d eval-llm eval-qrels gen-qrels

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


#   ┌─────────────────┬───────┬──────────────────────────────┐
#   │     Target      │ Judge │             Mode             │
#   ├─────────────────┼───────┼──────────────────────────────┤
#   │ make eval-llm   │ LLM   │ live Qdrant, silver standard │
#   ├─────────────────┼───────┼──────────────────────────────┤
#   │ make eval-qrels │ qrels │ live Qdrant, gold standard   │
#   └─────────────────┴───────┴──────────────────────────────┘

                                                            
# eval-llm — LLM judges relevance in real-time. No pre-built ground truth needed. Flexible but slow + costs tokens. "Silver standard" = LLM can be wrong.                                                                                                                 
eval-llm:
	EVAL_MODE=live EVAL_JUDGE=llm go test -v -timeout 600s -run TestSearchEval ./internal/pkb/
	
# eval-qrels — Pre-computed testdata/qrels.jsonl judges relevance. Fast, deterministic, reproducible. "Gold standard" = human-curated  correct answers.   
eval-qrels:
	EVAL_MODE=live go test -v -timeout 120s -run TestSearchEval ./internal/pkb/

gen-qrels:
	go run ./cmd/gen-qrels