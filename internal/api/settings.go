package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"nadir/config"
)

type settingsItem struct {
	Key     string
	Value   string
	FromEnv bool
}

type settingsGroup struct {
	Name  string
	Items []settingsItem
}

// Settings renders a read-only view of the effective config this server
// booted with — config.yaml values plus whatever applyEnv() overrode, since
// there is no other way for a user to see what's actually running without
// reading source. Nothing here is editable: config is loaded once at
// startup (config.Load) and never re-read, so the panel says so explicitly
// rather than implying a live editor.
func (d *dependencies) Settings(c *gin.Context) {
	d.renderHTML(c, http.StatusOK, "settings", struct{ Groups []settingsGroup }{buildSettingsGroups(d.cfg)})
}

func buildSettingsGroups(cfg *config.Config) []settingsGroup {
	if cfg == nil {
		return nil
	}

	// FromEnv comes from cfg.Overridden — the record applyEnv builds at
	// boot — so a newly added env var is picked up without editing this
	// file.
	it := func(key, value string) settingsItem {
		_, fromEnv := cfg.Overridden[key]
		return settingsItem{Key: key, Value: value, FromEnv: fromEnv}
	}

	apiKey := "(not set)"
	if cfg.Embedder.APIKey != "" {
		apiKey = "(set)"
	}

	return []settingsGroup{
		{Name: "HTTP", Items: []settingsItem{
			it("http.addr", cfg.HTTP.Addr),
			it("http.read_timeout", cfg.HTTP.ReadTimeout.String()),
			it("http.write_timeout", cfg.HTTP.WriteTimeout.String()),
			it("http.idle_timeout", cfg.HTTP.IdleTimeout.String()),
		}},
		{Name: "Middleware", Items: []settingsItem{
			it("middleware.timeout", cfg.Middleware.Timeout.String()),
			it("middleware.logger.level", cfg.Middleware.Logger.Level),
		}},
		{Name: "Source", Items: []settingsItem{
			it("source.paths", strings.Join(cfg.Source.Paths, ", ")),
		}},
		{Name: "Ingest", Items: []settingsItem{
			it("ingest.max_attempts", strconv.FormatUint(cfg.Ingest.MaxAttempts, 10)),
			it("ingest.initial_interval", cfg.Ingest.InitialInterval.String()),
			it("ingest.max_interval", cfg.Ingest.MaxInterval.String()),
			it("ingest.multiplier", strconv.FormatFloat(cfg.Ingest.Multiplier, 'f', -1, 64)),
		}},
		{Name: "Qdrant", Items: []settingsItem{
			it("qdrant.addr", cfg.Qdrant.Addr),
			it("qdrant.collection", cfg.Qdrant.Collection),
			it("qdrant.top_k", strconv.Itoa(cfg.Qdrant.TopK)),
			it("qdrant.prefetch_mul", strconv.Itoa(cfg.Qdrant.PrefetchMul)),
		}},
		{Name: "Embedder", Items: []settingsItem{
			it("embedder.provider", cfg.Embedder.Provider),
			it("embedder.model", cfg.Embedder.Model),
			it("embedder.api_key", apiKey),
			it("embedder.ollama_addr", cfg.Embedder.OllamaAddr),
			it("embedder.dimensions", strconv.Itoa(cfg.Embedder.Dimensions)),
			it("embedder.query_prefix", cfg.Embedder.QueryPrefix),
			it("embedder.document_prefix", cfg.Embedder.DocumentPrefix),
		}},
		{Name: "Chunker", Items: []settingsItem{
			it("chunker.provider", cfg.Chunker.Provider),
			it("chunker.chunk_size", strconv.Itoa(cfg.Chunker.ChunkSize)),
			it("chunker.chunk_overlap", strconv.Itoa(cfg.Chunker.ChunkOverlap)),
			it("chunker.window_size", strconv.Itoa(cfg.Chunker.WindowSize)),
		}},
		{Name: "Reranker", Items: []settingsItem{
			it("reranker.enabled", strconv.FormatBool(cfg.Reranker.Enabled)),
			it("reranker.addr", cfg.Reranker.Addr),
			it("reranker.model", cfg.Reranker.Model),
			it("reranker.candidate_mul", strconv.Itoa(cfg.Reranker.CandidateMul)),
			it("reranker.max_concurrent", strconv.Itoa(cfg.Reranker.MaxConcurrent)),
		}},
		{Name: "Semantic cache", Items: []settingsItem{
			it("semantic_cache.enabled", strconv.FormatBool(cfg.SemanticCache.Enabled)),
			it("semantic_cache.collection", cfg.SemanticCache.Collection),
			it("semantic_cache.threshold", strconv.FormatFloat(float64(cfg.SemanticCache.Threshold), 'f', -1, 32)),
			it("semantic_cache.ttl", cfg.SemanticCache.TTL.String()),
		}},
		{Name: "Generator", Items: []settingsItem{
			it("generator.enabled", strconv.FormatBool(cfg.Generator.Enabled)),
			it("generator.ollama_addr", cfg.Generator.OllamaAddr),
			it("generator.model", cfg.Generator.Model),
			it("generator.max_context_tokens", strconv.Itoa(cfg.Generator.MaxContextTokens)),
		}},
		{Name: "Rewriter", Items: []settingsItem{
			it("rewriter.enabled", strconv.FormatBool(cfg.Rewriter.Enabled)),
			it("rewriter.turns", strconv.Itoa(cfg.Rewriter.Turns)),
			it("rewriter.ollama_addr", cfg.Rewriter.OllamaAddr),
			it("rewriter.model", cfg.Rewriter.Model),
		}},
		{Name: "History", Items: []settingsItem{
			it("history.enabled", strconv.FormatBool(cfg.History.Enabled)),
			it("history.collection", cfg.History.Collection),
		}},
		{Name: "Enrichment", Items: []settingsItem{
			it("enrichment.hype.enabled", strconv.FormatBool(cfg.Enrichment.Hype.Enabled)),
			it("enrichment.hype.questions_per_chunk", strconv.Itoa(cfg.Enrichment.Hype.QuestionsPerChunk)),
			it("enrichment.hype.model", cfg.Enrichment.Hype.Model),
			it("enrichment.contextual.enabled", strconv.FormatBool(cfg.Enrichment.Contextual.Enabled)),
			it("enrichment.contextual.model", cfg.Enrichment.Contextual.Model),
		}},
	}
}
