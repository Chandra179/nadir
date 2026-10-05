// Command evaluator measures Retrieval quality over a golden query set
// against the configured Qdrant collection and optional reranker.
//
// Usage:
//
//	go run ./cmd/evaluator --golden path/to/questions.json [--config internal/bootstrap/configuration/config.yaml]
//	                         [--top-k N] [--no-rerank] [--runs N]
//	                         [--report path] [--ensure-ingest]
//	                         [--require-release-gate] [--validate-only]
//	                         [--generation-eval --judge-addr URL --judge-model MODEL --judge-suitability TEXT]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "nadir/internal/bootstrap/configuration"
	"nadir/internal/bootstrap/logging"
	"nadir/internal/bootstrap/runtime"
	"nadir/internal/core/documents/indexing"
	"nadir/internal/eval"
	ollamagenerator "nadir/internal/providers/ollama/generator"
	"nadir/internal/providers/reranker"

	"log/slog"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to config file")
	goldenPath := flag.String("golden", "", "required path to a golden query set for the configured corpus")
	topK := flag.Int("top-k", 0, "results per query (0 uses qdrant.top_k)")
	noRerank := flag.Bool("no-rerank", false, "bypass the configured reranker")
	runs := flag.Int("runs", 3, "runs per query; retain all rankings, median run quality, pooled request latency")
	reportPath := flag.String("report", "", "output report path (default: .local/evaluation/<unix_ts>.json)")
	ensureIngest := flag.Bool("ensure-ingest", false, "run an ingest pass over documents.paths before evaluating")
	requireReleaseGate := flag.Bool("require-release-gate", false, "reject synthetic/unconsented golden sets")
	validateOnly := flag.Bool("validate-only", false, "validate the golden set and release-gate metadata without starting external services")
	generationEval := flag.Bool("generation-eval", false, "also generate answers and judge faithfulness, answer relevancy, context precision, and context recall")
	judgeAddr := flag.String("judge-addr", "", "required Ollama address for the independently configured generation judge")
	judgeModel := flag.String("judge-model", "", "required Ollama judge model; serving metadata is recorded")
	judgeSuitability := flag.String("judge-suitability", "", "required acknowledgement describing why the judge is suitable and its calibration limits")
	judgeNumCtx := flag.Int("judge-num-ctx", 8192, "independent Ollama judge window; rejects prompts that would overflow")
	contextBudget := flag.Int("context-budget", 0, "generation-eval admitted-context budget in tokens (0 uses chat.max_context_tokens); the context-selection experiment arm")
	flag.Parse()

	if err := runWithOptions(*configPath, *goldenPath, *topK, *noRerank, *runs, *reportPath, *ensureIngest, *requireReleaseGate, *validateOnly, generationOptions{
		Enabled:          *generationEval,
		JudgeAddr:        *judgeAddr,
		JudgeModel:       *judgeModel,
		JudgeSuitability: *judgeSuitability,
		JudgeNumCtx:      *judgeNumCtx,
		ContextBudget:    *contextBudget,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "evaluator:", err)
		os.Exit(1)
	}
}

func run(configPath, goldenPath string, topK int, noRerank bool, runs int, reportPath string, ensureIngest, requireReleaseGate, validateOnly bool) error {
	return runWithOptions(configPath, goldenPath, topK, noRerank, runs, reportPath, ensureIngest, requireReleaseGate, validateOnly, generationOptions{})
}

type generationOptions struct {
	Enabled          bool
	JudgeAddr        string
	JudgeModel       string
	JudgeSuitability string
	JudgeNumCtx      int
	ContextBudget    int
}

func runWithOptions(configPath, goldenPath string, topK int, noRerank bool, runs int, reportPath string, ensureIngest, requireReleaseGate, validateOnly bool, generationOptions generationOptions) error {
	if strings.TrimSpace(goldenPath) == "" {
		return fmt.Errorf("--golden is required; provide a query set for the configured corpus")
	}
	golden, err := evaluation.LoadGoldenSet(goldenPath)
	if err != nil {
		return err
	}
	if requireReleaseGate {
		if err := golden.ValidateReleaseGate(); err != nil {
			return fmt.Errorf("release gate: %w", err)
		}
	}
	if validateOnly {
		fmt.Printf("golden set schema valid: %d queries (%s), release_gate=%v\n", len(golden.Queries), golden.Metadata.Dataset, golden.Metadata.ReleaseGate)
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := validateGenerationOptions(cfg, generationOptions); err != nil {
		return err
	}
	if generationOptions.Enabled && generationOptions.JudgeNumCtx == 0 {
		generationOptions.JudgeNumCtx = 8192
	}
	log, err := logger.New("info")
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	ctx := context.Background()
	if topK <= 0 {
		topK = cfg.Qdrant.TopK
	}
	if topK > cfg.Search.MaxTopK {
		topK = cfg.Search.MaxTopK
	}
	if runs < 1 {
		runs = 1
	}
	if cfg.Search.MaxTopK < 10 {
		return fmt.Errorf("evaluation MRR@10 requires search.max_top_k >= 10, got %d", cfg.Search.MaxTopK)
	}
	provenance, err := captureProvenance(ctx, cfg, goldenPath, golden, generationOptions, noRerank, topK, runs)
	if err != nil {
		return fmt.Errorf("report provenance: %w", err)
	}
	graph, err := runtime.NewDependencies(ctx, cfg, log, runtime.Options{
		DisableReranker:      noRerank,
		DisableSemanticCache: true,
	})
	if err != nil {
		return fmt.Errorf("shared runtime: %w", err)
	}
	defer func() { _ = graph.Close() }()

	stats, err := graph.Stats(ctx)
	if err != nil {
		return fmt.Errorf("collection stats: %w", err)
	}
	if stats.Chunks == 0 || ensureIngest {
		if err := ensureIngested(ctx, cfg, graph.Ingest, log); err != nil {
			return err
		}
	}

	rerankEnabled := cfg.Reranker.Enabled && !noRerank

	report, err := evaluation.NewDependencies(evaluation.DependenciesConfig{
		Searcher: graph.Searcher,
		Log:      log,
	}).Run(ctx, golden, topK, runs)
	if err != nil {
		return err
	}
	report.Rerank = rerankEnabled
	report.Provenance = provenance
	report.AdaptiveRerank = rerankEnabled && cfg.Reranker.AdaptiveEnabled
	if rerankEnabled {
		recordRerankerProfile(ctx, graph.RerankerProbe, report, log)
	}
	if generationOptions.Enabled {
		answerEndpoint := cfg.GeneratorEndpoint()
		answerGenerator := ollamagenerator.NewDependencies(ollamagenerator.DependenciesConfig{
			Addr:           answerEndpoint.Addr,
			Model:          answerEndpoint.Model,
			RequestTimeout: cfg.Generator.RequestTimeout,
			KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
			Options: map[string]any{
				"temperature": 0,
				"num_predict": cfg.Generator.MaxOutputTokens,
				"num_ctx":     cfg.Generator.NumCtx,
			},
		})
		judgeGenerator := ollamagenerator.NewDependencies(ollamagenerator.DependenciesConfig{
			Addr:           strings.TrimSpace(generationOptions.JudgeAddr),
			Model:          strings.TrimSpace(generationOptions.JudgeModel),
			RequestTimeout: cfg.Generator.RequestTimeout,
			KeepAlive:      cfg.Inference.Ollama.KeepAlive.String(),
			Format:         evaluation.JudgeResponseSchema(),
			Options: map[string]any{
				"temperature": 0,
				"num_predict": 128,
				"num_ctx":     generationOptions.JudgeNumCtx,
			},
		})
		generationReport, generationErr := evaluation.NewGenerationDependencies(evaluation.GenerationDependenciesConfig{
			Searcher:                 graph.Searcher,
			AnswerGenerator:          answerGenerator,
			JudgeGenerator:           judgeGenerator,
			AnswerModel:              answerEndpoint.Model,
			JudgeModel:               strings.TrimSpace(generationOptions.JudgeModel),
			JudgeSuitability:         generationOptions.JudgeSuitability,
			ModelFingerprints:        provenance.Models,
			MaxContextTokens:         cfg.Chat.MaxContextTokens,
			ContextBudgetOverride:    generationOptions.ContextBudget,
			ContextWindowTokens:      cfg.Generator.NumCtx,
			ReservedOutputTokens:     cfg.Generator.MaxOutputTokens,
			JudgeContextWindowTokens: generationOptions.JudgeNumCtx,
			RequestTimeout:           cfg.Generator.RequestTimeout,
			Log:                      log,
		}).Run(ctx, golden, topK)
		if generationErr != nil {
			return generationErr
		}
		report.Generation = generationReport
	}
	printReport(report)

	if reportPath == "" {
		reportPath = filepath.Join(".local", "evaluation", fmt.Sprintf("%d.json", time.Now().Unix()))
	}
	if dir := filepath.Dir(reportPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	if err := evaluation.WriteReport(reportPath, report); err != nil {
		return err
	}
	fmt.Println("\nreport written to", reportPath)
	return nil
}

// recordRerankerProfile captures the reranker profile actually serving the
// run for report provenance. A probe failure is non-fatal: the run continues
// and the report omits the profile.
func recordRerankerProfile(ctx context.Context, probe func(context.Context) (reranker.ProbeResult, error), report *evaluation.Report, log *slog.Logger) {
	if probe == nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := probe(probeCtx)
	if err != nil {
		log.Warn("reranker probe failed; report will omit the serving profile", slog.Any("error", err))
		return
	}
	report.RerankerModel = result.LoadedModel
	report.RerankerBackend = result.Backend
	report.RerankerDevice = result.Device
}

func validateGenerationOptions(cfg *config.Config, options generationOptions) error {
	if !options.Enabled {
		return nil
	}
	if cfg == nil || !cfg.Generator.Enabled {
		return fmt.Errorf("--generation-eval requires generator.enabled=true")
	}
	if strings.TrimSpace(options.JudgeAddr) == "" || strings.TrimSpace(options.JudgeModel) == "" {
		return fmt.Errorf("--generation-eval requires --judge-addr and --judge-model; judge configuration has no fallback")
	}
	if strings.TrimSpace(options.JudgeSuitability) == "" {
		return fmt.Errorf("--generation-eval requires --judge-suitability; judge size alone does not establish calibration")
	}
	if options.JudgeNumCtx < 0 {
		return fmt.Errorf("--judge-num-ctx must be positive (zero uses the 8192-token default)")
	}
	if evaluation.SameModelName(options.JudgeModel, cfg.Generator.Model) {
		return fmt.Errorf("--judge-model must differ from generator.model")
	}
	return nil
}

func ensureIngested(ctx context.Context, cfg *config.Config, ing indexing.Ingest, log *slog.Logger) error {
	if len(cfg.Documents.Paths) == 0 {
		return fmt.Errorf("collection is empty or --ensure-ingest was requested, but documents.paths has no directories")
	}
	files, err := indexing.DiscoverFiles(cfg.Documents.Paths, cfg.Documents.IgnorePatterns, cfg.Ingest.MaxFileBytes)
	if err != nil {
		return fmt.Errorf("discover source files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported source files found under %v", cfg.Documents.Paths)
	}

	log.Info("ingesting source files before evaluation", slog.Int("files", len(files)), slog.Any("roots", cfg.Documents.Paths))
	options := indexing.RunOptions{}
	if cfg.Documents.Mode == config.DocumentsModeMirror {
		options = indexing.RunOptions{MirrorSources: true, SourceRoots: cfg.Documents.Paths}
	}
	result, err := ing.Run(ctx, files, options)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}
	log.Info("ingest finished", slog.Int("processed", result.Processed), slog.Int("skipped", result.Skipped), slog.Int("failed", result.Failed))
	return nil
}

func printReport(report *evaluation.Report) {
	fmt.Printf("\n%-28s %-6s %-10s %-8s\n", "QUERY", "RANK", "FOUND", "MS")
	fmt.Println(strings.Repeat("-", 56))
	for _, query := range report.PerQuery {
		rank := "-"
		if query.FirstHitRank > 0 {
			rank = fmt.Sprintf("%d", query.FirstHitRank)
		}
		fmt.Printf("%-28s %-6s %d/%-8d %.1f\n", truncate(query.ID, 28), rank, query.RelevantFound, query.NumRelevant, query.LatencyMS)
	}
	aggregate := report.Aggregate
	fmt.Println(strings.Repeat("-", 56))
	fmt.Printf("HitRate@%d      %.3f\n", aggregate.TopK, aggregate.HitRateAtK)
	fmt.Printf("Recall@%d       %.3f\n", aggregate.TopK, aggregate.RecallAtK)
	fmt.Printf("MRR@10         %.3f\n", aggregate.MRRAt10)
	fmt.Printf("nDCG@%d         %.3f\n", aggregate.TopK, aggregate.NDCGAtK)
	fmt.Printf("distractor@%d  %.3f\n", aggregate.TopK, aggregate.DistractorHitRateAtK)
	fmt.Printf("latency p50/p95  %.1fms / %.1fms\n", aggregate.P50LatMS, aggregate.P95LatMS)
	fmt.Printf("rerank coverage %.1f%% calls=%d candidates=%d p50/p95=%.1fms / %.1fms errors=%d\n",
		aggregate.RerankCoverage*100, aggregate.RerankDependencyCalls, aggregate.RerankCandidateTotal,
		aggregate.RerankP50LatMS, aggregate.RerankP95LatMS, aggregate.RerankErrors)
	fmt.Printf("queries=%d requests=%d reranker=%v adaptive=%v top_k=%d retrieval_depth=%d\n", aggregate.Queries, aggregate.Requests, report.Rerank, report.AdaptiveRerank, aggregate.TopK, report.RetrievalDepth)
	if report.RerankerBackend != "" || report.RerankerDevice != "" {
		fmt.Printf("reranker profile  model=%s backend=%s device=%s\n", report.RerankerModel, report.RerankerBackend, report.RerankerDevice)
	}
	if report.Generation != nil {
		generation := report.Generation
		fmt.Printf("generation answer=%s judge=%s calibration=%s coverage=%.1f%% failures=%d\n",
			generation.AnswerModel, generation.JudgeModel, generation.CalibrationStatus,
			generation.Aggregate.JudgeCoverage*100, generation.Aggregate.Failures)
		fmt.Printf("faithfulness=%.3f relevancy=%.3f context_precision=%.3f context_recall=%.3f\n",
			generation.Aggregate.Faithfulness, generation.Aggregate.AnswerRelevancy,
			generation.Aggregate.ContextPrecision, generation.Aggregate.ContextRecall)
		fmt.Printf("generation p50/p95=%.1fms / %.1fms judge p50/p95=%.1fms / %.1fms\n",
			generation.Aggregate.AnswerP50LatMS, generation.Aggregate.AnswerP95LatMS,
			generation.Aggregate.JudgeP50LatMS, generation.Aggregate.JudgeP95LatMS)
		if len(generation.Aggregate.FailureClasses) > 0 {
			fmt.Printf("generation failure classes=%v\n", generation.Aggregate.FailureClasses)
		}
		if len(generation.Aggregate.DiagnosticCauses) > 0 {
			fmt.Printf("generation diagnostic causes=%v\n", generation.Aggregate.DiagnosticCauses)
		}
	}
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	return value[:width-1] + "~"
}
