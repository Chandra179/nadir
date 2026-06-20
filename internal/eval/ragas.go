package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"nadir/internal/pkb"
)

// ---------------------------------------------------------------------------
// LLM-as-judge interface
// ---------------------------------------------------------------------------

// LLMJudge is the abstraction for any LLM that can answer a prompt with text.
// The stub implementation in ragas_test.go satisfies this without real HTTP.
type LLMJudge interface {
	Judge(ctx context.Context, prompt string) (string, error)
}

// OllamaJudge calls an OpenAI-compatible /v1/chat/completions endpoint (Ollama exposes this).
type OllamaJudge struct {
	BaseURL string
	Model   string
	APIKey  string
	client  *http.Client
}

func NewOllamaJudge(baseURL, model, apiKey string) *OllamaJudge {
	return &OllamaJudge{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		APIKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (j *OllamaJudge) Judge(ctx context.Context, prompt string) (string, error) {
	reqBody := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}{
		Model: j.Model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ragas judge marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("ragas judge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if j.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.APIKey)
	}

	resp, err := j.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ragas judge call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ragas judge HTTP %d", resp.StatusCode)
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("ragas judge decode: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("ragas judge: empty choices")
	}
	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

// ---------------------------------------------------------------------------
// Answer generator interface (reuses pkb.Generator)
// ---------------------------------------------------------------------------

// AnswerGenerator generates a grounded answer from retrieved chunks.
// pkb.Generator satisfies this via an adapter.
type AnswerGenerator interface {
	Generate(ctx context.Context, query string, chunks []pkb.ScoredChunk) (string, error)
}

// GeneratorAdapter wraps pkb.Generator to satisfy AnswerGenerator.
type GeneratorAdapter struct {
	Gen pkb.Generator
}

func (g *GeneratorAdapter) Generate(ctx context.Context, query string, chunks []pkb.ScoredChunk) (string, error) {
	rc, err := g.Gen.Generate(ctx, query, chunks)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// ---------------------------------------------------------------------------
// RAGAS report types
// ---------------------------------------------------------------------------

// RAGASReport aggregates RAGAS metrics over a golden set.
type RAGASReport struct {
	NumQueries      int
	Faithfulness    float64
	AnswerRelevance float64
	ContextPrecision float64
	ContextRecall   float64
	PerQuery        []RAGASQueryReport
}

// RAGASQueryReport is the per-query RAGAS breakdown.
type RAGASQueryReport struct {
	Query            string
	Answer           string
	Faithfulness     float64
	AnswerRelevance  float64
	ContextPrecision float64
	ContextRecall    float64
}

// ---------------------------------------------------------------------------
// RAGAS evaluator — implements metrics from Es et al. 2023 (arxiv 2309.15217)
// ---------------------------------------------------------------------------

type RAGASEvaluator struct {
	Judge     LLMJudge
	Generator AnswerGenerator
}

// Evaluate runs RAGAS metrics over every golden query.
// For each query it: retrieves chunks, generates an answer, then scores 4 metrics.
// ContextRecall requires ExpectedAnswer in the golden set; queries without it
// get ContextRecall = -1 (skipped).
func (e *RAGASEvaluator) Evaluate(ctx context.Context, gs *GoldenSet, searcher Retriever, fetchK int) (RAGASReport, error) {
	if fetchK <= 0 {
		fetchK = 5
	}
	rep := RAGASReport{NumQueries: len(gs.Queries)}

	var sumFaith, sumAnsRel, sumCtxPrec, sumCtxRecall float64
	ctxRecallCount := 0

	for _, gq := range gs.Queries {
		chunks, err := searcher.Search(ctx, gq.Query, fetchK, nil)
		if err != nil {
			return RAGASReport{}, fmt.Errorf("ragas: retrieve %q: %w", gq.Query, err)
		}

		answer, err := e.Generator.Generate(ctx, gq.Query, chunks)
		if err != nil {
			return RAGASReport{}, fmt.Errorf("ragas: generate %q: %w", gq.Query, err)
		}

		contextText := buildChunkContext(chunks)

		qr := RAGASQueryReport{
			Query:  gq.Query,
			Answer: answer,
		}

		qr.Faithfulness, err = e.scoreFaithfulness(ctx, answer, contextText)
		if err != nil {
			return RAGASReport{}, fmt.Errorf("ragas: faithfulness %q: %w", gq.Query, err)
		}

		qr.AnswerRelevance, err = e.scoreAnswerRelevance(ctx, gq.Query, answer)
		if err != nil {
			return RAGASReport{}, fmt.Errorf("ragas: answer relevance %q: %w", gq.Query, err)
		}

		qr.ContextPrecision, err = e.scoreContextPrecision(ctx, gq.Query, chunks)
		if err != nil {
			return RAGASReport{}, fmt.Errorf("ragas: context precision %q: %w", gq.Query, err)
		}

		if gq.ExpectedAnswer != "" {
			qr.ContextRecall, err = e.scoreContextRecall(ctx, gq.ExpectedAnswer, contextText)
			if err != nil {
				return RAGASReport{}, fmt.Errorf("ragas: context recall %q: %w", gq.Query, err)
			}
			sumCtxRecall += qr.ContextRecall
			ctxRecallCount++
		} else {
			qr.ContextRecall = -1
		}

		sumFaith += qr.Faithfulness
		sumAnsRel += qr.AnswerRelevance
		sumCtxPrec += qr.ContextPrecision

		rep.PerQuery = append(rep.PerQuery, qr)
	}

	if rep.NumQueries > 0 {
		rep.Faithfulness = sumFaith / float64(rep.NumQueries)
		rep.AnswerRelevance = sumAnsRel / float64(rep.NumQueries)
		rep.ContextPrecision = sumCtxPrec / float64(rep.NumQueries)
	}
	if ctxRecallCount > 0 {
		rep.ContextRecall = sumCtxRecall / float64(ctxRecallCount)
	}
	return rep, nil
}

// ---------------------------------------------------------------------------
// Metric 1: Faithfulness (RAGAS §3.1)
// Decompose answer into statements, verify each against context.
// Faithfulness = supported statements / total statements.
// ---------------------------------------------------------------------------

func (e *RAGASEvaluator) scoreFaithfulness(ctx context.Context, answer, contextText string) (float64, error) {
	if answer == "" {
		return 0, nil
	}

	statements, err := e.extractStatements(ctx, answer)
	if err != nil {
		return 0, err
	}
	if len(statements) == 0 {
		return 0, nil
	}

	supported, err := e.verifyStatements(ctx, statements, contextText)
	if err != nil {
		return 0, err
	}
	if len(supported) != len(statements) {
		return 0, nil
	}

	count := 0
	for _, s := range supported {
		if s {
			count++
		}
	}
	return float64(count) / float64(len(statements)), nil
}

const extractStatementsPrompt = `Extract all factual claims from the following answer. Each claim should be a single self-contained sentence.

Answer:
%s

Return one claim per line. Do not number them. Do not add commentary.`

func (e *RAGASEvaluator) extractStatements(ctx context.Context, answer string) ([]string, error) {
	prompt := fmt.Sprintf(extractStatementsPrompt, answer)
	raw, err := e.Judge.Judge(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("extract statements: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

const verifyStatementsPrompt = `You are a fact-checker. For each statement, determine if it is supported by the context below.

Context:
%s

Statements:
%s

Return ONLY a JSON array of booleans (true or false), one per statement, in order.
Example for 3 statements: [true, false, true]`

func (e *RAGASEvaluator) verifyStatements(ctx context.Context, statements []string, contextText string) ([]bool, error) {
	var sb strings.Builder
	for i, s := range statements {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, s)
	}

	prompt := fmt.Sprintf(verifyStatementsPrompt, contextText, sb.String())
	raw, err := e.Judge.Judge(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("verify statements: %w", err)
	}

	raw = cleanJSON(raw)
	var flags []bool
	if err := json.NewDecoder(bytes.NewBufferString(raw)).Decode(&flags); err != nil {
		return nil, fmt.Errorf("verify statements parse: %w (raw: %q)", err, raw)
	}
	return flags, nil
}

// ---------------------------------------------------------------------------
// Metric 2: Answer Relevance (RAGAS §3.2, simplified)
// Direct LLM judgment: "does this answer address the question?" on 0-1 scale.
// ---------------------------------------------------------------------------

const answerRelevancePrompt = `Rate how relevant the answer is to the question.
A relevant answer directly addresses the question without irrelevant information or evasion.

Question: %s
Answer: %s

Return ONLY a single number between 0 and 1 (where 1 = perfectly relevant, 0 = not relevant at all).`

func (e *RAGASEvaluator) scoreAnswerRelevance(ctx context.Context, query, answer string) (float64, error) {
	if answer == "" {
		return 0, nil
	}
	prompt := fmt.Sprintf(answerRelevancePrompt, query, answer)
	raw, err := e.Judge.Judge(ctx, prompt)
	if err != nil {
		return 0, fmt.Errorf("answer relevance: %w", err)
	}
	return parseScore(raw)
}

// ---------------------------------------------------------------------------
// Metric 3: Context Precision (RAGAS §3.3)
// LLM rates each retrieved chunk's relevance to the query.
// Score = mean of per-chunk relevance scores (0-1 each).
// ---------------------------------------------------------------------------

const contextPrecisionPrompt = `Rate each context passage for relevance to the question.
Return ONLY a JSON array of numbers between 0 and 1, one per passage, in order.

Question: %s

Passages:
%s

Example for 3 passages: [0.9, 0.1, 0.7]`

func (e *RAGASEvaluator) scoreContextPrecision(ctx context.Context, query string, chunks []pkb.ScoredChunk) (float64, error) {
	if len(chunks) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, text)
	}

	prompt := fmt.Sprintf(contextPrecisionPrompt, query, sb.String())
	raw, err := e.Judge.Judge(ctx, prompt)
	if err != nil {
		return 0, fmt.Errorf("context precision: %w", err)
	}

	raw = cleanJSON(raw)
	var scores []float64
	if err := json.NewDecoder(bytes.NewBufferString(raw)).Decode(&scores); err != nil {
		return 0, fmt.Errorf("context precision parse: %w (raw: %q)", err, raw)
	}
	if len(scores) == 0 {
		return 0, nil
	}

	// Weight by position: earlier chunks should be more relevant (RAGAS uses 1/log2(k+1) weighting)
	var weightedSum, weightSum float64
	for i, s := range scores {
		w := 1.0 / math.Log2(float64(i+2))
		weightedSum += s * w
		weightSum += w
	}
	if weightSum == 0 {
		return 0, nil
	}
	return weightedSum / weightSum, nil
}

// ---------------------------------------------------------------------------
// Metric 4: Context Recall (RAGAS §3.4)
// Decompose expected answer into statements, check if each is attributable to context.
// Context Recall = attributable statements / total statements.
// ---------------------------------------------------------------------------

func (e *RAGASEvaluator) scoreContextRecall(ctx context.Context, expectedAnswer, contextText string) (float64, error) {
	if expectedAnswer == "" {
		return 0, nil
	}

	statements, err := e.extractStatements(ctx, expectedAnswer)
	if err != nil {
		return 0, err
	}
	if len(statements) == 0 {
		return 0, nil
	}

	// Reuse the verify pattern but with different prompt framing
	prompt := fmt.Sprintf(`You are a fact-checker. For each statement, determine if it can be attributed to (i.e. supported by) the context below.

Context:
%s

Statements:
%s

Return ONLY a JSON array of booleans (true or false), one per statement, in order.
Example for 3 statements: [true, false, true]`, contextText, formatStatements(statements))

	raw, err := e.Judge.Judge(ctx, prompt)
	if err != nil {
		return 0, fmt.Errorf("context recall: %w", err)
	}

	raw = cleanJSON(raw)
	var flags []bool
	if err := json.NewDecoder(bytes.NewBufferString(raw)).Decode(&flags); err != nil {
		return 0, fmt.Errorf("context recall parse: %w (raw: %q)", err, raw)
	}
	if len(flags) != len(statements) {
		return 0, nil
	}

	count := 0
	for _, f := range flags {
		if f {
			count++
		}
	}
	return float64(count) / float64(len(statements)), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func buildChunkContext(chunks []pkb.ScoredChunk) string {
	var sb strings.Builder
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		fmt.Fprintf(&sb, "[%d] (source: %s)\n%s\n\n", i+1, c.FilePath, text)
	}
	return sb.String()
}

func formatStatements(statements []string) string {
	var sb strings.Builder
	for i, s := range statements {
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, s)
	}
	return sb.String()
}

func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	return raw
}

func parseScore(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	// Try to extract a number from the response
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err != nil {
		return 0, fmt.Errorf("parse score from %q: %w", raw, err)
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = f / 100 // handle 0-100 scale
		if f > 1 {
			f = 1
		}
	}
	return f, nil
}

// PrintRAGASReport writes a human-readable RAGAS summary to w.
func PrintRAGASReport(w io.Writer, rep RAGASReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Queries:\t%d\n", rep.NumQueries)
	fmt.Fprintf(tw, "Faithfulness:\t%.4f\n", rep.Faithfulness)
	fmt.Fprintf(tw, "Answer Relevance:\t%.4f\n", rep.AnswerRelevance)
	fmt.Fprintf(tw, "Context Precision:\t%.4f\n", rep.ContextPrecision)
	fmt.Fprintf(tw, "Context Recall:\t%.4f\n", rep.ContextRecall)
	tw.Flush()

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Per-query:")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "QUERY\tFAITH\tANS_REL\tCTX_PREC\tCTX_REC\tANSWER (truncated)")
	for _, q := range rep.PerQuery {
		ctxRec := fmt.Sprintf("%.3f", q.ContextRecall)
		if q.ContextRecall < 0 {
			ctxRec = "N/A"
		}
		fmt.Fprintf(tw, "%s\t%.3f\t%.3f\t%.3f\t%s\t%s\n",
			truncate(q.Query, 30),
			q.Faithfulness, q.AnswerRelevance, q.ContextPrecision, ctxRec,
			truncate(q.Answer, 60))
	}
	tw.Flush()
}

// AggregateRAGAS builds a RAGASReport from per-query results.
// Exported for testing.
func AggregateRAGAS(results []RAGASQueryReport) RAGASReport {
	rep := RAGASReport{NumQueries: len(results), PerQuery: results}
	if len(results) == 0 {
		return rep
	}
	var sumF, sumA, sumP float64
	var sumR float64
	rCount := 0
	for _, q := range results {
		sumF += q.Faithfulness
		sumA += q.AnswerRelevance
		sumP += q.ContextPrecision
		if q.ContextRecall >= 0 {
			sumR += q.ContextRecall
			rCount++
		}
	}
	rep.Faithfulness = sumF / float64(len(results))
	rep.AnswerRelevance = sumA / float64(len(results))
	rep.ContextPrecision = sumP / float64(len(results))
	if rCount > 0 {
		rep.ContextRecall = sumR / float64(rCount)
	}
	return rep
}

// SortRAGASByQuery sorts per-query results alphabetically for stable output.
func SortRAGASByQuery(rep *RAGASReport) {
	sort.Slice(rep.PerQuery, func(i, j int) bool {
		return rep.PerQuery[i].Query < rep.PerQuery[j].Query
	})
}
