// gen-qrels generates testdata/qrels.jsonl from eval_queries.jsonl by
// searching live Qdrant and marking retrieved chunks as relevant.
// Run: go run ./cmd/gen-qrels
// Prereq: make up && make ingest
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"nadir/config"
	"nadir/internal/pkb"
)

func main() {
	cfgPath := flag.String("config", "config/config.yaml", "path to config.yaml")
	queriesPath := flag.String("queries", "internal/pkb/testdata/eval_queries.jsonl", "eval queries jsonl")
	outPath := flag.String("out", "internal/pkb/testdata/qrels.jsonl", "output qrels jsonl")
	topK := flag.Int("top-k", 10, "chunks retrieved per query (all marked relevant)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	embedder := pkb.NewOllamaEmbedder(cfg.Embedder.OllamaAddr, cfg.Embedder.Model, cfg.Embedder.Dimensions)

	store, err := pkb.NewQdrantStore(cfg.Qdrant.Addr, cfg.Qdrant.Collection, cfg.Qdrant.PrefetchMul)
	if err != nil {
		log.Fatalf("qdrant: %v", err)
	}

	qf, err := os.Open(*queriesPath)
	if err != nil {
		log.Fatalf("open queries: %v", err)
	}
	defer qf.Close()

	var cases []pkb.EvalCase
	sc := bufio.NewScanner(qf)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var c pkb.EvalCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			log.Fatalf("parse query: %v", err)
		}
		cases = append(cases, c)
	}
	if err := sc.Err(); err != nil {
		log.Fatalf("read queries: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	out, err := os.Create(*outPath)
	if err != nil {
		log.Fatalf("create qrels: %v", err)
	}
	defer out.Close()

	enc := json.NewEncoder(out)
	ctx := context.Background()

	for _, c := range cases {
		vec, err := embedder.Embed(ctx, c.Query)
		if err != nil {
			log.Printf("warn: embed %q: %v", c.Query, err)
			continue
		}
		hits, err := store.HybridSearch(ctx, vec, c.Query, *topK, nil)
		if err != nil {
			log.Printf("warn: search %q: %v", c.Query, err)
			continue
		}
		for _, h := range hits {
			q := pkb.Qrel{
				Query:    c.Query,
				ChunkID:  h.Key(),
				FilePath: h.FilePath,
				Relevant: true,
			}
			if err := enc.Encode(q); err != nil {
				log.Fatalf("write qrel: %v", err)
			}
		}
		log.Printf("query %q -> %d qrels", c.Query, len(hits))
	}
	log.Printf("done: wrote %s", *outPath)
}
