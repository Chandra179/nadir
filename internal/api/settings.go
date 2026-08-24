package api

import (
	"net/http"
	"os"
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

// envOverridden mirrors the exact set of vars config.Config.applyEnv reads,
// so the panel can flag which values came from the environment at boot
// rather than from config.yaml.
var envOverridden = map[string]string{
	"qdrant.addr":              "QDRANT_ADDR",
	"qdrant.collection":        "QDRANT_COLLECTION",
	"embedder.ollama_addr":     "OLLAMA_ADDR",
	"embedder.api_key":         "EMBEDDER_API_KEY",
	"reranker.addr":            "RERANKER_ADDR",
	"reranker.enabled":         "RERANKER_ENABLED",
	"middleware.logger.level":  "LOGGER_LEVEL",
	"semantic_cache.threshold": "SEMANTIC_CACHE_THRESHOLD",
}

func item(key, value string) settingsItem {
	envVar, ok := envOverridden[key]
	return settingsItem{Key: key, Value: value, FromEnv: ok && os.Getenv(envVar) != ""}
}

func buildSettingsGroups(cfg *config.Config) []settingsGroup {
	if cfg == nil {
		return nil
	}

	apiKey := "(not set)"
	if cfg.Embedder.APIKey != "" {
		apiKey = "(set)"
	}

	return []settingsGroup{
		{Name: "HTTP", Items: []settingsItem{
			item("http.addr", cfg.HTTP.Addr),
			item("http.read_timeout", cfg.HTTP.ReadTimeout.String()),
			item("http.write_timeout", cfg.HTTP.WriteTimeout.String()),
			item("http.idle_timeout", cfg.HTTP.IdleTimeout.String()),
		}},
		{Name: "Middleware", Items: []settingsItem{
			item("middleware.timeout", cfg.Middleware.Timeout.String()),
			item("middleware.logger.level", cfg.Middleware.Logger.Level),
		}},
		{Name: "Source", Items: []settingsItem{
			item("source.paths", strings.Join(cfg.Source.Paths, ", ")),
		}},
		{Name: "Ingest", Items: []settingsItem{
			item("ingest.max_attempts", strconv.FormatUint(cfg.Ingest.MaxAttempts, 10)),
			item("ingest.initial_interval", cfg.Ingest.InitialInterval.String()),
			item("ingest.max_interval", cfg.Ingest.MaxInterval.String()),
			item("ingest.multiplier", strconv.FormatFloat(cfg.Ingest.Multiplier, 'f', -1, 64)),
		}},
		{Name: "Qdrant", Items: []settingsItem{
			item("qdrant.addr", cfg.Qdrant.Addr),
			item("qdrant.collection", cfg.Qdrant.Collection),
			item("qdrant.top_k", strconv.Itoa(cfg.Qdrant.TopK)),
			item("qdrant.prefetch_mul", strconv.Itoa(cfg.Qdrant.PrefetchMul)),
		}},
		{Name: "Embedder", Items: []settingsItem{
			item("embedder.provider", cfg.Embedder.Provider),
			item("embedder.model", cfg.Embedder.Model),
			item("embedder.api_key", apiKey),
			item("embedder.ollama_addr", cfg.Embedder.OllamaAddr),
			item("embedder.dimensions", strconv.Itoa(cfg.Embedder.Dimensions)),
		}},
		{Name: "Chunker", Items: []settingsItem{
			item("chunker.provider", cfg.Chunker.Provider),
			item("chunker.chunk_size", strconv.Itoa(cfg.Chunker.ChunkSize)),
			item("chunker.chunk_overlap", strconv.Itoa(cfg.Chunker.ChunkOverlap)),
			item("chunker.window_size", strconv.Itoa(cfg.Chunker.WindowSize)),
		}},
		{Name: "Reranker", Items: []settingsItem{
			item("reranker.enabled", strconv.FormatBool(cfg.Reranker.Enabled)),
			item("reranker.addr", cfg.Reranker.Addr),
			item("reranker.candidate_mul", strconv.Itoa(cfg.Reranker.CandidateMul)),
			item("reranker.max_concurrent", strconv.Itoa(cfg.Reranker.MaxConcurrent)),
		}},
		{Name: "Semantic cache", Items: []settingsItem{
			item("semantic_cache.enabled", strconv.FormatBool(cfg.SemanticCache.Enabled)),
			item("semantic_cache.collection", cfg.SemanticCache.Collection),
			item("semantic_cache.threshold", strconv.FormatFloat(float64(cfg.SemanticCache.Threshold), 'f', -1, 32)),
			item("semantic_cache.ttl", cfg.SemanticCache.TTL.String()),
		}},
		{Name: "Generator", Items: []settingsItem{
			item("generator.enabled", strconv.FormatBool(cfg.Generator.Enabled)),
			item("generator.ollama_addr", cfg.Generator.OllamaAddr),
			item("generator.model", cfg.Generator.Model),
			item("generator.max_context_tokens", strconv.Itoa(cfg.Generator.MaxContextTokens)),
		}},
	}
}
