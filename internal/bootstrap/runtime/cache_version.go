package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	config "nadir/internal/bootstrap/configuration"
)

// cachePolicyVersion fingerprints the ranking and embedding contract. Corpus
// freshness is a separate cache generation; credentials never enter this
// policy identity. JSON gives deterministic ordering to fusion-profile maps.
// The version prefix changes when retrieval behavior changes without a config
// change: v3 keys entries by the question's first fragment vector and records
// the request size, and the per-file cap now backfills from lower ranks.
func cachePolicyVersion(cfg *config.Config) string {
	embedding := cfg.Embedder
	embedding.APIKey = ""
	ranking := cfg.Reranker
	if !ranking.Enabled {
		ranking = config.RerankerConfig{}
	}
	policy := struct {
		Documents config.QdrantConfig
		Embedding config.EmbedderConfig
		Search    config.SearchConfig
		Reranker  config.RerankerConfig
	}{cfg.Qdrant, embedding, cfg.Search, ranking}
	// The configuration contains only scalar values and a string-keyed map;
	// encoding this concrete shape cannot fail.
	raw, _ := json.Marshal(policy)
	return fmt.Sprintf("retrieval-v3:%x", sha256.Sum256(raw))
}
