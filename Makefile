.PHONY: dev

dev:
	./scripts/dev-local.sh

make eval golden=golden-samples.yaml          # retrieval metrics
make eval-rag golden=golden-samples.yaml      # RAGAS eval
make eval-both golden=golden-samples.yaml     # both in one pass