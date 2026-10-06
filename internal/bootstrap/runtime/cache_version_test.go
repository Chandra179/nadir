package runtime

import (
	"testing"

	config "nadir/internal/bootstrap/configuration"
)

func TestCachePolicyVersionChangesWithRetrievalContract(t *testing.T) {
	base := config.Config{
		Embedder: config.EmbedderConfig{Model: "embedding", Dimensions: 768},
		Qdrant:   config.QdrantConfig{Collection: "documents", PrefetchMul: 5},
		Search:   config.SearchConfig{MaxFragments: 16, MaxChunksPerFile: 3},
		Reranker: config.RerankerConfig{Enabled: true, Model: "ranker", CandidateMul: 3},
	}
	want := cachePolicyVersion(&base)
	for _, test := range []struct {
		name string
		edit func(*config.Config)
	}{
		{"embedding model", func(c *config.Config) { c.Embedder.Model = "other" }},
		{"embedding prefixes", func(c *config.Config) { c.Embedder.DocumentPrefix = "document: " }},
		{"document corpus", func(c *config.Config) { c.Qdrant.Collection = "other" }},
		{"prefetch depth", func(c *config.Config) { c.Qdrant.PrefetchMul++ }},
		{"fragment limit", func(c *config.Config) { c.Search.MaxFragments++ }},
		{"file diversity", func(c *config.Config) { c.Search.MaxChunksPerFile++ }},
		{"fusion weights", func(c *config.Config) { c.Search.Fusion.DenseWeight = 2 }},
		{"fusion profiles", func(c *config.Config) {
			c.Search.Fusion.Profiles = map[string]config.FusionProfileConfig{"formula": {DenseWeight: 2}}
		}},
		{"reranker disabled", func(c *config.Config) { c.Reranker.Enabled = false }},
		{"reranker model", func(c *config.Config) { c.Reranker.Model = "other" }},
		{"reranker candidate pool", func(c *config.Config) { c.Reranker.CandidateMul++ }},
		{"adaptive reranking", func(c *config.Config) { c.Reranker.AdaptiveEnabled = true }},
		{"adaptive threshold", func(c *config.Config) { c.Reranker.AdaptiveMarginThreshold = 0.1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.edit(&changed)
			if got := cachePolicyVersion(&changed); got == want {
				t.Fatal("retrieval policy change reused the same cache identity")
			}
		})
	}
}

func TestCachePolicyVersionIgnoresCredentialsAndMapInsertionOrder(t *testing.T) {
	first := config.Config{Search: config.SearchConfig{Fusion: config.FusionConfig{Profiles: map[string]config.FusionProfileConfig{
		"formula": {DenseWeight: 2}, "factoid": {DenseWeight: 1},
	}}}}
	second := first
	second.Embedder.APIKey = "credential"
	second.Search.Fusion.Profiles = map[string]config.FusionProfileConfig{
		"factoid": {DenseWeight: 1}, "formula": {DenseWeight: 2},
	}
	if cachePolicyVersion(&first) != cachePolicyVersion(&second) {
		t.Fatal("equivalent retrieval policies produced different cache identities")
	}
}
