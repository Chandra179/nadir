//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"syscall/js"
	"time"

	"nadir/internal/bootstrap/assembly"
	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/readiness"
	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/providers/browser"
	memorydocuments "nadir/internal/providers/memory/documents"
	memoryhistory "nadir/internal/providers/memory/history"
)

// main registers globalThis.nadir.start and parks the Go runtime so exported
// callbacks stay alive.
func main() {
	api := js.Global().Get("Object").New()
	api.Set("start", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 {
			return rejected(fmt.Errorf("start(options) takes one argument"))
		}
		options := args[0]
		return browser.NewPromise(func() (any, error) { return start(options) })
	}))
	js.Global().Set("nadir", api)
	select {}
}

// start builds the runtime from host options:
//
//	{ dimensions, embed(text), embedBatch?(texts), model?,
//	  generate?(prompt, {signal, emit}), generatorModel?, contextWindow?, maxOutputTokens?,
//	  complete?(system, user)   // enables follow-up query rewriting
//	  rerank?(query, passages), convert?(name, bytes),
//	  snapshot?: {documents?, history?} }
//
// and resolves to a handle with fetch, ingest, query, versions, reset,
// snapshot, close. Optional roles are enabled by supplying their function.
func start(options js.Value) (any, error) {
	cfg, err := config.Default()
	if err != nil {
		return nil, fmt.Errorf("default config: %w", err)
	}
	dimensions := options.Get("dimensions").Int()
	embedder, err := browser.NewEmbedder(options.Get("embed"), options.Get("embedBatch"), dimensions)
	if err != nil {
		return nil, err
	}
	cfg.Embedder.Dimensions = dimensions
	if model := options.Get("model"); model.Type() == js.TypeString {
		cfg.Embedder.Model = model.String()
	}
	has := func(name string) bool { return options.Get(name).Type() == js.TypeFunction }

	// The browser runtime keeps its own storage and has no server-side roles.
	cfg.SemanticCache.Enabled = false
	cfg.Reranker.Enabled = has("rerank")
	cfg.Enrichment.Contextual.Enabled = false
	cfg.Generator.Enabled = has("generate")
	cfg.Rewriter.Enabled = has("complete") && cfg.Generator.Enabled
	cfg.History.Enabled = true
	cfg.Docling.Enabled = false
	if model := options.Get("generatorModel"); model.Type() == js.TypeString {
		cfg.Generator.Model = model.String()
	}
	if v := options.Get("contextWindow"); v.Type() == js.TypeNumber {
		cfg.Generator.NumCtx = v.Int()
	}
	if v := options.Get("maxOutputTokens"); v.Type() == js.TypeNumber {
		cfg.Generator.MaxOutputTokens = v.Int()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store := memorydocuments.New(memorydocuments.Config{PrefetchMul: cfg.Qdrant.PrefetchMul})
	chats := memoryhistory.New()
	if snap := options.Get("snapshot"); snap.Type() == js.TypeObject {
		if raw, ok := bytesOption(snap, "documents"); ok {
			if err := store.Restore(raw); err != nil {
				return nil, fmt.Errorf("restore documents: %w", err)
			}
		}
		if raw, ok := bytesOption(snap, "history"); ok {
			if err := chats.Restore(raw); err != nil {
				return nil, fmt.Errorf("restore history: %w", err)
			}
		}
	}

	prov := assembly.Providers{Store: store, Embedder: embedder}
	if has("rerank") {
		if prov.Reranker, err = browser.NewReranker(options.Get("rerank")); err != nil {
			return nil, err
		}
	}
	if has("convert") {
		if prov.Converter, err = browser.NewConverter(options.Get("convert")); err != nil {
			return nil, err
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	graph, err := assembly.Assemble(context.Background(), cfg, log, prov)
	if err != nil {
		return nil, err
	}

	api := assembly.APIProviders{History: chats}
	if has("generate") {
		if api.Generator, err = browser.NewGenerator(options.Get("generate")); err != nil {
			return nil, err
		}
	}
	if cfg.Rewriter.Enabled {
		if api.Rewriter, err = browser.NewRewriter(options.Get("complete")); err != nil {
			return nil, err
		}
	}
	api.Readiness = readinessChecks(cfg, graph)
	built := assembly.BuildAPI(cfg, graph, api, log)
	return handle(graph, built, store, chats), nil
}

// readinessChecks reports the in-page dependencies; host functions that were
// supplied are ready by construction.
func readinessChecks(cfg *config.Config, graph *assembly.Runtime) []readiness.Dependency {
	ready := func(model, details string) readiness.Probe {
		return func(context.Context) (readiness.Check, error) {
			return readiness.Check{Model: model, Details: details}, nil
		}
	}
	return []readiness.Dependency{
		{Name: "store", Probe: ready("", "in-memory")},
		{Name: "embedding", Probe: ready(cfg.Embedder.Model, fmt.Sprintf("dimensions=%d", graph.Embedder.Dimensions()))},
		{Name: "generator", Probe: ready(cfg.Generator.Model, ""), Disabled: !cfg.Generator.Enabled},
		{Name: "rewriter", Probe: ready(cfg.Generator.Model, ""), Disabled: !cfg.Rewriter.Enabled},
		{Name: "reranker", Probe: ready(cfg.Reranker.Model, ""), Disabled: !cfg.Reranker.Enabled},
	}
}

func bytesOption(obj js.Value, key string) ([]byte, bool) {
	v := obj.Get(key)
	if v.Type() != js.TypeObject {
		return nil, false
	}
	raw := make([]byte, v.Get("length").Int())
	js.CopyBytesToGo(raw, v)
	return raw, true
}

func handle(graph *assembly.Runtime, built *assembly.API, store *memorydocuments.Store, chats *memoryhistory.Store) js.Value {
	h := js.Global().Get("Object").New()
	set := func(name string, fn func(args []js.Value) (any, error)) {
		h.Set(name, js.FuncOf(func(_ js.Value, args []js.Value) any {
			// Copy argument values now: they are only valid during this call.
			captured := append([]js.Value(nil), args...)
			return browser.NewPromise(func() (any, error) { return fn(captured) })
		}))
	}
	ctx := context.Background()

	// ingest([{name, data: Uint8Array|string}]) → {operation_id, processed, ...}
	set("ingest", func(args []js.Value) (any, error) {
		files, err := uploadFiles(args)
		if err != nil {
			return nil, err
		}
		result, err := graph.Ingest.Run(ctx, files, indexing.RunOptions{})
		if err != nil {
			return nil, err
		}
		return toJS(struct {
			OperationID string                `json:"operation_id"`
			Processed   int                   `json:"processed"`
			Skipped     int                   `json:"skipped"`
			Failed      int                   `json:"failed"`
			Removed     int                   `json:"removed"`
			Files       []indexing.FileResult `json:"files"`
		}{result.OperationID, result.Processed, result.Skipped, result.Failed, result.Removed, result.Files})
	})

	// query({query, top_k?, filter?: {file_path, header, source_sha}}) → {results}
	set("query", func(args []js.Value) (any, error) {
		if len(args) != 1 || args[0].Type() != js.TypeObject {
			return nil, fmt.Errorf("query(request) takes one object")
		}
		request := search.Request{Query: args[0].Get("query").String(), TopK: 5, SkipCache: true}
		if topK := args[0].Get("top_k"); topK.Type() == js.TypeNumber {
			request.TopK = topK.Int()
		}
		if f := args[0].Get("filter"); f.Type() == js.TypeObject {
			request.Filter = &search.Filter{
				FilePath:  optionalString(f, "file_path"),
				Header:    optionalString(f, "header"),
				SourceSHA: optionalString(f, "source_sha"),
			}
		}
		result, err := graph.Searcher.Query(ctx, request)
		if err != nil {
			return nil, err
		}
		type chunkJSON struct {
			FilePath      string  `json:"file_path"`
			Header        string  `json:"header,omitempty"`
			LineStart     int     `json:"line_start"`
			ChunkIndex    int     `json:"chunk_index"`
			RetrievalRank int     `json:"retrieval_rank"`
			Score         float32 `json:"score"`
			SourceSHA     string  `json:"source_sha,omitempty"`
			Text          string  `json:"text"`
		}
		out := struct {
			Results []chunkJSON `json:"results"`
		}{Results: make([]chunkJSON, len(result.Chunks))}
		for i, c := range result.Chunks {
			out.Results[i] = chunkJSON{c.FilePath, c.Header, c.LineStart, c.ChunkIndex, i + 1, c.Score, c.SourceSHA, c.Text}
		}
		return toJS(out)
	})

	// versions() → {path: sha}
	set("versions", func([]js.Value) (any, error) {
		versions, err := graph.DocumentVersions(ctx)
		if err != nil {
			return nil, err
		}
		return toJS(versions)
	})

	set("reset", func([]js.Value) (any, error) {
		return nil, graph.Reset(ctx)
	})

	// fetch({method, url, headers?, body?, signal?, onHead, onChunk}) serves the
	// versioned JSON/SSE API in-page; see browser.Fetch.
	set("fetch", func(args []js.Value) (any, error) {
		if len(args) != 1 || args[0].Type() != js.TypeObject {
			return nil, fmt.Errorf("fetch(request) takes one object")
		}
		return nil, browser.Fetch(ctx, built.Mux, args[0])
	})

	// snapshot() → {documents, history} Uint8Arrays the host can persist and
	// pass back to start({snapshot}).
	set("snapshot", func([]js.Value) (any, error) {
		out := js.Global().Get("Object").New()
		for name, snapshot := range map[string]func() ([]byte, error){"documents": store.Snapshot, "history": chats.Snapshot} {
			raw, err := snapshot()
			if err != nil {
				return nil, err
			}
			array := js.Global().Get("Uint8Array").New(len(raw))
			js.CopyBytesToJS(array, raw)
			out.Set(name, array)
		}
		return out, nil
	})

	set("close", func([]js.Value) (any, error) {
		drainCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return nil, errors.Join(built.Drain(drainCtx), graph.Close())
	})
	return h
}

func uploadFiles(args []js.Value) ([]indexing.UploadFile, error) {
	if len(args) != 1 || args[0].Type() != js.TypeObject {
		return nil, fmt.Errorf("ingest(files) takes an array")
	}
	list := args[0]
	files := make([]indexing.UploadFile, list.Length())
	for i := range files {
		item := list.Index(i)
		name := item.Get("name")
		if name.Type() != js.TypeString {
			return nil, fmt.Errorf("file %d: name must be a string", i)
		}
		data := item.Get("data")
		switch data.Type() {
		case js.TypeString:
			files[i] = indexing.UploadFile{Name: name.String(), Data: []byte(data.String())}
		case js.TypeObject:
			raw := make([]byte, data.Get("length").Int())
			js.CopyBytesToGo(raw, data)
			files[i] = indexing.UploadFile{Name: name.String(), Data: raw}
		default:
			return nil, fmt.Errorf("file %d: data must be a string or Uint8Array", i)
		}
	}
	return files, nil
}

func optionalString(obj js.Value, key string) string {
	if v := obj.Get(key); v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

// toJS converts a JSON-tagged Go value to a plain JavaScript object.
func toJS(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return js.Global().Get("JSON").Call("parse", string(raw)), nil
}

func rejected(err error) js.Value {
	return js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New(err.Error()))
}
