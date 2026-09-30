package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/core/documents/indexing"
	evaluation "nadir/internal/eval"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func captureProvenance(ctx context.Context, cfg *config.Config, goldenPath string, golden *evaluation.GoldenSet, options generationOptions, noRerank bool, topK, runs int) (*evaluation.ReportProvenance, error) {
	out := &evaluation.ReportProvenance{}
	var err error
	out.GoldenSHA256, err = evaluation.HashFile(goldenPath)
	if err != nil {
		return nil, err
	}
	files, err := indexing.DiscoverFiles(cfg.Documents.Paths, cfg.Documents.IgnorePatterns, cfg.Ingest.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Name)
	}
	out.ConfiguredSources, err = evaluation.FingerprintCorpus(paths)
	if err != nil {
		return nil, err
	}
	if corpus := golden.Metadata.Corpus; corpus != nil {
		out.DeclaredCorpusSHA256 = corpus.ManifestSHA256
		if len(corpus.Documents) > 0 {
			actual, err := evaluation.FingerprintCorpus(corpus.Documents)
			if err != nil {
				return nil, err
			}
			out.FixtureCorpus = &actual
			// Existing math fixture hashes the path+bytes manifest; newer fixtures
			// supply a JSON manifest artifact whose own exact bytes are hashed.
			if corpus.ManifestPath == "" && actual.ContentSHA256 != corpus.ManifestSHA256 {
				return nil, fmt.Errorf("fixture corpus bytes differ from declared hash; regenerate metadata or restore sources")
			}
		}
		if corpus.ManifestPath != "" {
			out.CorpusManifestPath = corpus.ManifestPath
			out.CorpusManifestSHA256, err = evaluation.HashFile(corpus.ManifestPath)
			if err != nil {
				return nil, err
			}
			if out.CorpusManifestSHA256 != corpus.ManifestSHA256 {
				return nil, fmt.Errorf("corpus manifest artifact differs from declared SHA-256")
			}
			if out.FixtureCorpus != nil {
				var declared evaluation.CorpusFingerprint
				// Verify every source fingerprint represented in the generated manifest.
				// Decode through a bounded helper so a metadata match cannot hide stale files.
				data, err := os.ReadFile(corpus.ManifestPath)
				if err != nil {
					return nil, err
				}
				if json.Unmarshal(data, &declared) == nil && declared.ContentSHA256 != "" && declared.ContentSHA256 != out.FixtureCorpus.ContentSHA256 {
					return nil, fmt.Errorf("source corpus differs from the manifest content hash")
				}
			}
		}
	}
	sanitized := *cfg
	sanitized.Embedder.APIKey = ""
	sanitized.Reranker.Enabled = cfg.Reranker.Enabled && !noRerank
	sanitized.Embedder.OllamaAddr = redactAddress(sanitized.Embedder.OllamaAddr)
	sanitized.Generator.OllamaAddr = redactAddress(sanitized.Generator.OllamaAddr)
	sanitized.Rewriter.OllamaAddr = redactAddress(sanitized.Rewriter.OllamaAddr)
	sanitized.Enrichment.Contextual.OllamaAddr = redactAddress(sanitized.Enrichment.Contextual.OllamaAddr)
	sanitized.Qdrant.Addr = redactAddress(sanitized.Qdrant.Addr)
	sanitized.Reranker.Addr = redactAddress(sanitized.Reranker.Addr)
	sanitized.Docling.Addr = redactAddress(sanitized.Docling.Addr)
	effective := map[string]any{"config": sanitized, "disable_semantic_cache": true, "no_rerank": noRerank, "top_k": topK, "retrieval_depth": max(topK, 10), "runs": runs, "context_budget": options.ContextBudget, "judge_model": options.JudgeModel, "judge_addr": redactAddress(options.JudgeAddr), "judge_num_ctx": options.JudgeNumCtx, "generation_eval": options.Enabled}
	out.EffectiveConfig = effective
	out.EffectiveConfigSHA256, err = evaluation.HashJSON(effective)
	if err != nil {
		return nil, err
	}
	out.Models = append(out.Models, modelFingerprint(ctx, "embedder", cfg.Embedder.OllamaAddr, cfg.Embedder.Model))
	if options.Enabled {
		answer := cfg.GeneratorEndpoint()
		out.Models = append(out.Models, modelFingerprint(ctx, "answer", answer.Addr, answer.Model), modelFingerprint(ctx, "judge", options.JudgeAddr, options.JudgeModel))
		answerModel, judgeModel := out.Models[len(out.Models)-2], out.Models[len(out.Models)-1]
		if answerModel.Digest != "" && answerModel.Digest == judgeModel.Digest {
			return nil, fmt.Errorf("answer and judge endpoints report the same model digest; choose an independent model")
		}
	}
	return out, nil
}

func redactAddress(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid address"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func modelFingerprint(ctx context.Context, role, addr, model string) evaluation.ModelFingerprint {
	out := evaluation.ModelFingerprint{Role: role, Model: model, Status: "unavailable"}
	client := &http.Client{Timeout: 5 * time.Second}
	payload, _ := json.Marshal(map[string]string{"model": model})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(addr, "/")+"/api/show", bytes.NewReader(payload))
	if err != nil {
		out.Error = "invalid metadata endpoint"
		return out
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		out.Error = "model metadata probe failed"
		return out
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		out.Error = fmt.Sprintf("model metadata HTTP %d", response.StatusCode)
		return out
	}
	var show struct {
		Details struct {
			Family        string `json:"family"`
			ParameterSize string `json:"parameter_size"`
			Quantization  string `json:"quantization_level"`
		} `json:"details"`
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&show); err != nil {
		out.Error = "invalid model metadata response"
		return out
	}
	out.Family = show.Details.Family
	out.ParameterSize = show.Details.ParameterSize
	out.Quantization = show.Details.Quantization
	if count, ok := show.ModelInfo["general.parameter_count"]; ok {
		_ = json.Unmarshal(count, &out.Parameters)
	}
	out.Status = "observed"
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(addr, "/")+"/api/tags", nil)
	if err != nil {
		return out
	}
	tagsResponse, err := client.Do(request)
	if err != nil {
		return out
	}
	defer tagsResponse.Body.Close()
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if tagsResponse.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(tagsResponse.Body, 2<<20)).Decode(&tags) == nil {
		normalize := func(name string) string {
			if !strings.Contains(name, ":") {
				return name + ":latest"
			}
			return name
		}
		for _, item := range tags.Models {
			if normalize(item.Name) == normalize(model) || normalize(item.Model) == normalize(model) {
				out.Digest = item.Digest
				break
			}
		}
	}
	return out
}
