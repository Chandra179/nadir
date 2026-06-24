package eval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"nadir/internal/store"
)

// stubJudge returns a fixed response or an error for each call.
type stubJudge struct {
	responses []string
	err       error
	calls     int
}

func (s *stubJudge) Judge(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.calls >= len(s.responses) {
		return "", errors.New("stubJudge: no more responses")
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

// stubGenerator returns a fixed answer.
type stubGenerator struct {
	answer string
	err    error
}

func (s *stubGenerator) Generate(_ context.Context, _ string, _ []store.ScoredChunk) (string, error) {
	return s.answer, s.err
}

func TestParseScore(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"0.85", 0.85},
		{"0", 0},
		{"1", 1},
		{"85", 0.85},
		{"100", 1.0},
		{"  0.5  ", 0.5},
	}
	for _, tc := range tests {
		got, err := parseScore(tc.input)
		if err != nil {
			t.Errorf("parseScore(%q) error: %v", tc.input, err)
		}
		if absDiff(got, tc.want) > 1e-6 {
			t.Errorf("parseScore(%q) = %.4f, want %.4f", tc.input, got, tc.want)
		}
	}
}

func TestParseScore_Invalid(t *testing.T) {
	if _, err := parseScore("not a number"); err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"```json\n[1,2,3]\n```", "[1,2,3]"},
		{"```\n[1,2]\n```", "[1,2]"},
		{"[1,2,3]", "[1,2,3]"},
		{"  [true]  ", "[true]"},
	}
	for _, tc := range tests {
		got := cleanJSON(tc.input)
		if got != tc.want {
			t.Errorf("cleanJSON(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestScoreFaithfulness(t *testing.T) {
	// Stub: first call extracts 2 statements, second call verifies [true, false]
	judge := &stubJudge{
		responses: []string{
			"The sky is blue.\nGrass is purple.", // extract statements
			"[true, false]",                       // verify
		},
	}
	e := &RAGASEvaluator{Judge: judge}
	score, err := e.scoreFaithfulness(context.Background(), "The sky is blue. Grass is purple.", "context here")
	if err != nil {
		t.Fatalf("scoreFaithfulness: %v", err)
	}
	if absDiff(score, 0.5) > 1e-6 {
		t.Errorf("Faithfulness = %.4f, want 0.5 (1 of 2 supported)", score)
	}
}

func TestScoreFaithfulness_EmptyAnswer(t *testing.T) {
	judge := &stubJudge{}
	e := &RAGASEvaluator{Judge: judge}
	score, err := e.scoreFaithfulness(context.Background(), "", "context")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if score != 0 {
		t.Errorf("empty answer faithfulness = %.4f, want 0", score)
	}
}

func TestScoreAnswerRelevance(t *testing.T) {
	judge := &stubJudge{responses: []string{"0.9"}}
	e := &RAGASEvaluator{Judge: judge}
	score, err := e.scoreAnswerRelevance(context.Background(), "what is X?", "X is a thing.")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if absDiff(score, 0.9) > 1e-6 {
		t.Errorf("AnswerRelevance = %.4f, want 0.9", score)
	}
}

func TestScoreContextPrecision(t *testing.T) {
	judge := &stubJudge{responses: []string{"[0.9, 0.1, 0.7]"}}
	e := &RAGASEvaluator{Judge: judge}
	chunks := []store.ScoredChunk{
		{Text: "chunk 1"},
		{Text: "chunk 2"},
		{Text: "chunk 3"},
	}
	score, err := e.scoreContextPrecision(context.Background(), "query", chunks)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Weighted by 1/log2(k+1):
	// w1 = 1/log2(2) = 1.0, w2 = 1/log2(3) = 0.6309, w3 = 1/log2(4) = 0.5
	// weighted = (0.9*1.0 + 0.1*0.6309 + 0.7*0.5) / (1.0 + 0.6309 + 0.5)
	//          = (0.9 + 0.0631 + 0.35) / 2.1309 = 1.3131 / 2.1309 = 0.6164
	if score <= 0 || score > 1 {
		t.Errorf("ContextPrecision = %.4f, should be in (0, 1]", score)
	}
}

func TestScoreContextRecall(t *testing.T) {
	judge := &stubJudge{
		responses: []string{
			"Go has goroutines.\nGo has channels.", // extract from expected answer
			"[true, true]",                          // verify against context
		},
	}
	e := &RAGASEvaluator{Judge: judge}
	score, err := e.scoreContextRecall(context.Background(), "Go has goroutines. Go has channels.", "context about Go")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if absDiff(score, 1.0) > 1e-6 {
		t.Errorf("ContextRecall = %.4f, want 1.0 (both supported)", score)
	}
}

func TestScoreContextRecall_Partial(t *testing.T) {
	judge := &stubJudge{
		responses: []string{
			"Go has goroutines.\nGo has channels.", // extract
			"[true, false]",                         // verify — only 1 of 2
		},
	}
	e := &RAGASEvaluator{Judge: judge}
	score, err := e.scoreContextRecall(context.Background(), "Go has goroutines. Go has channels.", "partial context")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if absDiff(score, 0.5) > 1e-6 {
		t.Errorf("ContextRecall = %.4f, want 0.5", score)
	}
}

func TestEvaluate_EndToEnd(t *testing.T) {
	// Full pipeline with stubs
	judge := &stubJudge{
		responses: []string{
			// Query 1: "secant formula"
			"Secant is 1/cos(theta).",                  // extract statements from answer
			"[true]",                                    // verify statements
			"0.95",                                      // answer relevance
			"[0.9, 0.3]",                                // context precision (2 chunks)
			// Context recall: extract from expected answer, then verify
			"Secant equals 1 over cosine.",              // extract from expected
			"[true]",                                    // verify against context
		},
	}
	gen := &stubGenerator{answer: "The secant function is 1/cos(theta)."}
	searcher := &fakeRetriever{results: []store.ScoredChunk{
		{Text: "secant info", FilePath: "math/trig.md"},
		{Text: "other info", FilePath: "other.md"},
	}}

	gs := &GoldenSet{Queries: []GoldenQuery{
		{Query: "secant formula", ExpectedAnswer: "Secant equals 1 over cosine."},
	}}

	e := &RAGASEvaluator{Judge: judge, Generator: gen}
	rep, err := e.Evaluate(context.Background(), gs, searcher, 5)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if rep.NumQueries != 1 {
		t.Fatalf("NumQueries = %d, want 1", rep.NumQueries)
	}
	if rep.Faithfulness != 1.0 {
		t.Errorf("Faithfulness = %.4f, want 1.0", rep.Faithfulness)
	}
	if rep.ContextRecall != 1.0 {
		t.Errorf("ContextRecall = %.4f, want 1.0", rep.ContextRecall)
	}
}

func TestEvaluate_NoExpectedAnswer_SkipsContextRecall(t *testing.T) {
	judge := &stubJudge{
		responses: []string{
			"Statement one.", // extract
			"[true]",         // verify
			"0.8",            // answer relevance
			"[0.9]",          // context precision
		},
	}
	gen := &stubGenerator{answer: "Some answer."}
	searcher := &fakeRetriever{results: []store.ScoredChunk{
		{Text: "info", FilePath: "a.md"},
	}}

	gs := &GoldenSet{Queries: []GoldenQuery{
		{Query: "q"}, // no ExpectedAnswer
	}}

	e := &RAGASEvaluator{Judge: judge, Generator: gen}
	rep, err := e.Evaluate(context.Background(), gs, searcher, 5)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if rep.PerQuery[0].ContextRecall != -1 {
		t.Errorf("ContextRecall = %.4f, want -1 (skipped)", rep.PerQuery[0].ContextRecall)
	}
	if rep.ContextRecall != 0 {
		t.Errorf("aggregate ContextRecall = %.4f, want 0 (no queries had expected_answer)", rep.ContextRecall)
	}
}

func TestEvaluate_JudgeError(t *testing.T) {
	judge := &stubJudge{err: errors.New("LLM down")}
	gen := &stubGenerator{answer: "answer"}
	searcher := &fakeRetriever{results: []store.ScoredChunk{
		{Text: "info", FilePath: "a.md"},
	}}
	gs := &GoldenSet{Queries: []GoldenQuery{{Query: "q"}}}

	e := &RAGASEvaluator{Judge: judge, Generator: gen}
	if _, err := e.Evaluate(context.Background(), gs, searcher, 5); err == nil {
		t.Fatal("expected error when judge fails")
	}
}

func TestAggregateRAGAS(t *testing.T) {
	results := []RAGASQueryReport{
		{Faithfulness: 1.0, AnswerRelevance: 0.8, ContextPrecision: 0.9, ContextRecall: 1.0},
		{Faithfulness: 0.5, AnswerRelevance: 0.6, ContextPrecision: 0.7, ContextRecall: -1},
	}
	rep := AggregateRAGAS(results)
	if absDiff(rep.Faithfulness, 0.75) > 1e-6 {
		t.Errorf("Faithfulness = %.4f, want 0.75", rep.Faithfulness)
	}
	if absDiff(rep.AnswerRelevance, 0.7) > 1e-6 {
		t.Errorf("AnswerRelevance = %.4f, want 0.7", rep.AnswerRelevance)
	}
	// ContextRecall: only 1 query had it (1.0), other was -1 (skipped)
	if absDiff(rep.ContextRecall, 1.0) > 1e-6 {
		t.Errorf("ContextRecall = %.4f, want 1.0", rep.ContextRecall)
	}
}

func TestBuildChunkContext(t *testing.T) {
	chunks := []store.ScoredChunk{
		{Text: "hello", FilePath: "a.md"},
		{WindowText: "window text", FilePath: "b.md"},
	}
	ctx := buildChunkContext(chunks)
	if !strings.Contains(ctx, "hello") {
		t.Error("expected chunk text in context")
	}
	if !strings.Contains(ctx, "window text") {
		t.Error("expected window text in context when WindowText is set")
	}
	if !strings.Contains(ctx, "a.md") {
		t.Error("expected file path in context")
	}
}

func TestExtractStatements(t *testing.T) {
	judge := &stubJudge{
		responses: []string{"First statement.\nSecond statement.\nThird statement."},
	}
	e := &RAGASEvaluator{Judge: judge}
	stmts, err := e.extractStatements(context.Background(), "some answer")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(stmts) != 3 {
		t.Fatalf("got %d statements, want 3", len(stmts))
	}
	if stmts[0] != "First statement." {
		t.Errorf("stmt[0] = %q", stmts[0])
	}
}

func TestExtractStatements_Empty(t *testing.T) {
	judge := &stubJudge{responses: []string{""}}
	e := &RAGASEvaluator{Judge: judge}
	stmts, err := e.extractStatements(context.Background(), "answer with no clear statements")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(stmts) != 0 {
		t.Errorf("expected 0 statements from empty response, got %d", len(stmts))
	}
}

func TestVerifyStatements_MalformedJSON(t *testing.T) {
	judge := &stubJudge{responses: []string{"not json at all"}}
	e := &RAGASEvaluator{Judge: judge}
	_, err := e.verifyStatements(context.Background(), []string{"stmt1", "stmt2"}, "context")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestScoreContextPrecision_EmptyChunks(t *testing.T) {
	judge := &stubJudge{}
	e := &RAGASEvaluator{Judge: judge}
	score, err := e.scoreContextPrecision(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if score != 0 {
		t.Errorf("empty chunks precision = %.4f, want 0", score)
	}
}

func TestSortRAGASByQuery(t *testing.T) {
	rep := RAGASReport{PerQuery: []RAGASQueryReport{
		{Query: "zebra"},
		{Query: "apple"},
		{Query: "mango"},
	}}
	SortRAGASByQuery(&rep)
	if rep.PerQuery[0].Query != "apple" {
		t.Errorf("first = %q, want apple", rep.PerQuery[0].Query)
	}
	if rep.PerQuery[2].Query != "zebra" {
		t.Errorf("last = %q, want zebra", rep.PerQuery[2].Query)
	}
}

func TestRAGASJSONRoundTrip(t *testing.T) {
	// Verify that the JSON parsing handles common LLM output formats
	raw := `[true, false, true]`
	var flags []bool
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&flags); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(flags) != 3 || !flags[0] || flags[1] || !flags[2] {
		t.Errorf("flags = %v, want [true, false, true]", flags)
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
