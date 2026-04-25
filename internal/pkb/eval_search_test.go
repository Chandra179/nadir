package pkb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchEval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping eval: requires Docker + Ollama")
	}

	cfg := loadConfig(t)
	evalCfg := loadEvalConfig(t)

	ollamaAddr := os.Getenv("OLLAMA_ADDR")
	if ollamaAddr == "" {
		ollamaAddr = cfg.Embedder.OllamaAddr
	}

	ctx := context.Background()
	embedder := NewOllamaEmbedder(ollamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)
	judge, judgeName := buildJudge(t, evalCfg)
	topK := cfg.Qdrant.TopK
	evalCases := loadEvalCases(t)
	profiles := loadEvalProfiles(t)
	history := newEvalHistoryWriter(t, evalCfg)

	for _, profile := range profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			if profile.SparseScorer == "splade" && !checkSPLADESidecar(cfg.SparseScorer.Addr) {
				t.Skipf("splade sidecar not reachable at %s — run: python cmd/splade/main.py", cfg.SparseScorer.Addr)
			}

			store, skipIngest, mode, collection := buildStore(t, ctx, embedder, cfg)
			rerankerName, reranker := buildReranker(t, profile, cfg)

			var sparseEmb SparseEmbedder
			if profile.SparseScorer == "splade" {
				sparseEmb = NewSPLADESparseScorer(cfg.SparseScorer.Addr)
				if qs, ok := store.(*QdrantStore); ok {
					store = qs.WithSparseEmbedder(sparseEmb)
				}
			} else {
				if qs, ok := store.(*QdrantStore); ok {
					store = qs.WithSparseScorer(buildSparseScorer(ctx, profile, cfg))
				}
			}

			docsIngested := 0
			if !skipIngest {
				chunker := buildChunker(profile, cfg)
				pipeline := NewPipeline(chunker, embedder, store, cfg.Retry)
				if sparseEmb != nil {
					pipeline.WithSparseEmbedder(sparseEmb)
				}

				gitbookRoot := filepath.Join("..", "..", "gitbook")
				err := filepath.Walk(gitbookRoot, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return nil
					}
					if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
						return nil
					}
					content, readErr := os.ReadFile(path)
					if readErr != nil {
						return nil
					}
					rel, _ := filepath.Rel(gitbookRoot, path)
					if ingestErr := pipeline.Ingest(ctx, rel, string(content), "eval"); ingestErr != nil {
						t.Logf("warn: ingest %s: %v", rel, ingestErr)
					} else {
						docsIngested++
					}
					return nil
				})
				if err != nil {
					t.Fatalf("walk gitbook: %v", err)
				}
				t.Logf("ingested %d files", docsIngested)
			}

			var vectorCount int64
			if qs, ok := store.(*QdrantStore); ok {
				if n, err := qs.PointCount(ctx); err == nil {
					vectorCount = n
				} else {
					t.Logf("warn: point count: %v", err)
				}
			}

			hydeSearcher := buildHyDE(t, profile, cfg, embedder, store)

			qrelsTotal, qrelsRelevant := qrelsStats(judge)
			metrics := runEval(t, ctx, evalCases, embedder, store, reranker, judge, topK, cfg.Reranker.CandidateMul, hydeSearcher)

			fmt.Printf("\n=== Search Eval: %s (model=%s topK=%d) ===\n", profile.Name, cfg.Embedder.Model, topK)
			fmt.Printf("SparseScorer:  %s\n", profile.SparseScorer)
			fmt.Printf("Reranker:      %s\n", rerankerName)
			fmt.Printf("Chunker:       %s  ChunkSize: %d  ChunkOverlap: %d\n",
				profile.ChunkerProvider, profile.ChunkSize, profile.ChunkOverlap)
			fmt.Printf("Queries:       %d  DocsIngested: %d  Vectors: %d\n", len(evalCases), docsIngested, vectorCount)
			fmt.Printf("Qrels:         total=%d relevant=%d\n", qrelsTotal, qrelsRelevant)
			fmt.Printf("MRR@%d:          %.4f\n", topK, metrics.MRR)
			fmt.Printf("HitRate@%d:      %.4f\n", topK, metrics.HitRate)
			fmt.Printf("NDCG@%d:         %.4f\n", topK, metrics.NDCG)
			fmt.Printf("Precision@%d:    %.4f\n\n", topK, metrics.Precision)

			history.write(t, evalHistoryEntry{
				HyDE: profile.HyDE,
				Timestamp:       time.Now().UTC().Format(time.RFC3339),
				Profile:         profile.Name,
				SparseScorer:    profile.SparseScorer,
				Reranker:        rerankerName,
				ChunkSize:       profile.ChunkSize,
				ChunkOverlap:    profile.ChunkOverlap,
				ChunkerProvider: profile.ChunkerProvider,
				Mode:            mode,
				Judge:           judgeName,
				Collection:      collection,
				Model:           cfg.Embedder.Model,
				EmbedderDims:    cfg.Embedder.Dimensions,
				Queries:         len(evalCases),
				TopK:            topK,
				DocsIngested:    docsIngested,
				VectorCount:     vectorCount,
				QrelsTotal:      qrelsTotal,
				QrelsRelevant:   qrelsRelevant,
				CandidateMul:    cfg.Reranker.CandidateMul,
				MRR:             metrics.MRR,
				HitRate:         metrics.HitRate,
				NDCG:            metrics.NDCG,
				Precision:       metrics.Precision,
			})
		})
	}
}
