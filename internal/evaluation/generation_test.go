package evaluation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	conversationgeneration "nadir/internal/conversation/generation"
	"nadir/internal/retrieval/search"
)

type generationTestRetriever struct {
	chunks []search.Chunk
	err    error
}

func (r generationTestRetriever) Query(_ context.Context, _ search.Request) (search.Result, error) {
	return search.Result{Chunks: r.chunks}, r.err
}

type generationTestGenerator struct {
	response string
	err      error
	prompts  []string
}

func (g *generationTestGenerator) Generate(_ context.Context, prompt string) (<-chan conversationgeneration.Event, error) {
	g.prompts = append(g.prompts, prompt)
	if g.err != nil {
		return nil, g.err
	}
	stream := make(chan conversationgeneration.Event, 2)
	stream <- conversationgeneration.Event{Kind: conversationgeneration.EventToken, Text: g.response}
	stream <- conversationgeneration.Event{Kind: conversationgeneration.EventDone}
	close(stream)
	return stream, nil
}

func TestGenerationHarnessScoresAnswerAndContext(t *testing.T) {
	answer := &generationTestGenerator{response: "The derivative is supported by the context."}
	judge := &generationTestGenerator{response: `{"faithfulness":0.9,"answer_relevancy":0.8,"context_precision":0.7,"context_recall":0.6}`}
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:         generationTestRetriever{chunks: []search.Chunk{{FilePath: "calculus.md", Text: "The derivative is supported by the context."}}},
		AnswerGenerator:  answer,
		JudgeGenerator:   judge,
		AnswerModel:      "small-answer",
		JudgeModel:       "large-judge",
		JudgeModelLarger: true,
		MaxContextTokens: 100,
	})
	report, err := harness.Run(context.Background(), &GoldenSet{
		SchemaVersion: 2,
		Queries: []GoldenQuery{{
			ID: "derivative", Query: "what is the derivative", Type: QueryTypeFormula,
			FaithfulnessLabel: FaithfulnessFullySupported,
			ExpectedAnswer:    "The derivative is supported by the context.",
			RequiredClaims:    []string{"the derivative is supported"},
			Relevant:          []RelevantChunk{{File: "calculus.md"}},
		}},
	}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Aggregate.Evaluated != 1 || report.Aggregate.Failures != 0 {
		t.Fatalf("aggregate = %+v, want one successful evaluation", report.Aggregate)
	}
	if report.Aggregate.Faithfulness != 0.9 || report.Aggregate.ContextRecall != 0.6 {
		t.Fatalf("aggregate scores = %+v", report.Aggregate)
	}
	result := report.PerQuery[0]
	if result.AnswerStatus != "success" || result.JudgeStatus != "success" || result.AnswerBytes == 0 ||
		result.ContextChunks != 1 || result.ContextTokens == 0 || result.ContextTruncated {
		t.Fatalf("generation diagnostics = %+v", result)
	}
	if len(answer.prompts) != 1 || !strings.Contains(answer.prompts[0], "Answer:") {
		t.Fatalf("answer prompt = %q, want production prompt", answer.prompts)
	}
	if len(judge.prompts) != 1 || !strings.Contains(judge.prompts[0], "Required claims:") {
		t.Fatalf("judge prompt = %q, want evaluation rubric", judge.prompts)
	}
}

func TestGenerationHarnessRecordsJudgeFailures(t *testing.T) {
	answer := &generationTestGenerator{response: "answer"}
	judge := &generationTestGenerator{response: "not json"}
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:         generationTestRetriever{chunks: []search.Chunk{{FilePath: "doc.md", Text: "evidence"}}},
		AnswerGenerator:  answer,
		JudgeGenerator:   judge,
		AnswerModel:      "small-answer",
		JudgeModel:       "large-judge",
		JudgeModelLarger: true,
	})
	report, err := harness.Run(context.Background(), &GoldenSet{
		SchemaVersion: 2,
		Queries: []GoldenQuery{{
			ID: "q", Query: "question", ExpectedAnswer: "answer",
			RequiredClaims: []string{"claim"}, Relevant: []RelevantChunk{{File: "doc.md"}},
		}},
	}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Aggregate.Evaluated != 0 || report.Aggregate.Failures != 1 || report.Aggregate.JudgeCoverage != 0 {
		t.Fatalf("aggregate = %+v, want one judge failure", report.Aggregate)
	}
	if !strings.Contains(report.PerQuery[0].Error, "judge response") {
		t.Fatalf("error = %q, want judge response error", report.PerQuery[0].Error)
	}
	if report.PerQuery[0].FailureClass != failureJudge || report.PerQuery[0].JudgeStatus != "invalid_response" {
		t.Fatalf("judge diagnostics = %+v, want judge_failure/invalid_response", report.PerQuery[0])
	}
}

func TestGenerationHarnessClassifiesRetrievalMiss(t *testing.T) {
	answer := &generationTestGenerator{response: "The answer is supported."}
	judge := &generationTestGenerator{response: `{"faithfulness":0.9,"answer_relevancy":0.9,"context_precision":1,"context_recall":0}`}
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:         generationTestRetriever{chunks: []search.Chunk{{FilePath: "other.md", Text: "unrelated"}}},
		AnswerGenerator:  answer,
		JudgeGenerator:   judge,
		AnswerModel:      "small-answer",
		JudgeModel:       "large-judge",
		JudgeModelLarger: true,
		RequestTimeout:   time.Second,
	})
	report, err := harness.Run(context.Background(), &GoldenSet{
		SchemaVersion: 2,
		Queries: []GoldenQuery{{
			ID: "miss", Query: "question", ExpectedAnswer: "answer", RequiredClaims: []string{"claim"},
			Relevant: []RelevantChunk{{File: "missing.md", Contains: "evidence"}},
		}},
	}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := report.PerQuery[0].DiagnosticCause; got != failureRetrievalMiss {
		t.Fatalf("diagnostic cause = %q, want retrieval_miss", got)
	}
}

func TestGenerationHarnessClassifiesRetrievalError(t *testing.T) {
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:         generationTestRetriever{err: errors.New("qdrant unavailable")},
		AnswerGenerator:  &generationTestGenerator{response: "answer"},
		JudgeGenerator:   &generationTestGenerator{response: `{"faithfulness":1,"answer_relevancy":1,"context_precision":1,"context_recall":1}`},
		AnswerModel:      "small-answer",
		JudgeModel:       "large-judge",
		JudgeModelLarger: true,
		RequestTimeout:   time.Second,
	})
	report, err := harness.Run(context.Background(), &GoldenSet{
		SchemaVersion: 2,
		Queries: []GoldenQuery{{
			ID: "retrieval-error", Query: "question", ExpectedAnswer: "answer", RequiredClaims: []string{"claim"},
			Relevant: []RelevantChunk{{File: "doc.md", Contains: "evidence"}},
		}},
	}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := report.PerQuery[0]
	if result.FailureClass != failureRetrievalMiss || result.AnswerStatus != "not_run" || result.JudgeStatus != "not_run" {
		t.Fatalf("retrieval error diagnostics = %+v, want retrieval_miss/not_run/not_run", result)
	}
}

func TestGenerationHarnessClassifiesAnswerTimeout(t *testing.T) {
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:         generationTestRetriever{chunks: []search.Chunk{{FilePath: "doc.md", Text: "evidence"}}},
		AnswerGenerator:  blockingGenerationTestGenerator{},
		JudgeGenerator:   &generationTestGenerator{response: `{"faithfulness":1,"answer_relevancy":1,"context_precision":1,"context_recall":1}`},
		AnswerModel:      "small-answer",
		JudgeModel:       "large-judge",
		JudgeModelLarger: true,
		RequestTimeout:   5 * time.Millisecond,
	})
	report, err := harness.Run(context.Background(), &GoldenSet{
		SchemaVersion: 2,
		Queries: []GoldenQuery{{
			ID: "timeout", Query: "question", ExpectedAnswer: "answer", RequiredClaims: []string{"claim"},
			Relevant: []RelevantChunk{{File: "doc.md", Contains: "evidence"}},
		}},
	}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := report.PerQuery[0]
	if result.FailureClass != failureTimeout || result.AnswerStatus != "error" {
		t.Fatalf("timeout diagnostics = %+v, want timeout/error", result)
	}
}

func TestGenerationHarnessClassifiesJudgeContractFailure(t *testing.T) {
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:         generationTestRetriever{chunks: []search.Chunk{{FilePath: "doc.md", Text: "evidence"}}},
		AnswerGenerator:  &generationTestGenerator{response: "answer"},
		JudgeGenerator:   &generationTestGenerator{response: `{"faithfulness":1,"answer_relevancy":1,"context_precision":1,"context_recall":3}`},
		AnswerModel:      "small-answer",
		JudgeModel:       "large-judge",
		JudgeModelLarger: true,
		RequestTimeout:   time.Second,
	})
	report, err := harness.Run(context.Background(), &GoldenSet{
		SchemaVersion: 2,
		Queries: []GoldenQuery{{
			ID: "judge-contract", Query: "question", ExpectedAnswer: "answer", RequiredClaims: []string{"claim"},
			Relevant: []RelevantChunk{{File: "doc.md", Contains: "evidence"}},
		}},
	}, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	result := report.PerQuery[0]
	if result.FailureClass != failureJudge || result.JudgeStatus != "invalid_response" {
		t.Fatalf("judge contract diagnostics = %+v, want judge_failure/invalid_response", result)
	}
}

func TestGenerationHarnessRecordsQualityDiagnosticCause(t *testing.T) {
	tests := []struct {
		name       string
		faithful   float64
		context    float64
		maxContext int
		want       string
	}{
		{name: "context selection", faithful: 0.9, context: 0.4, maxContext: 100, want: failureContextSelect},
		{name: "prompt generation", faithful: 0.4, context: 1, maxContext: 100, want: failurePromptGenerate},
		{name: "context truncation", faithful: 0.9, context: 1, maxContext: 10, want: failureContextSelect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := NewGenerationDependencies(GenerationDependenciesConfig{
				Searcher: generationTestRetriever{chunks: []search.Chunk{{
					FilePath: "doc.md", Header: "Evidence", Text: "evidence supports the claim",
				}}},
				AnswerGenerator:  &generationTestGenerator{response: "answer"},
				JudgeGenerator:   &generationTestGenerator{response: fmt.Sprintf(`{"faithfulness":%v,"answer_relevancy":1,"context_precision":1,"context_recall":%v}`, tt.faithful, tt.context)},
				AnswerModel:      "small-answer",
				JudgeModel:       "large-judge",
				JudgeModelLarger: true,
				MaxContextTokens: tt.maxContext,
				RequestTimeout:   time.Second,
			})
			report, err := harness.Run(context.Background(), &GoldenSet{
				SchemaVersion: 2,
				Queries: []GoldenQuery{{
					ID: tt.name, Query: "question", ExpectedAnswer: "answer", RequiredClaims: []string{"claim"},
					Relevant: []RelevantChunk{{File: "doc.md", Contains: "evidence"}},
				}},
			}, 1)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := report.PerQuery[0].DiagnosticCause; got != tt.want {
				t.Fatalf("diagnostic cause = %q, want %q; result=%+v", got, tt.want, report.PerQuery[0])
			}
		})
	}
}

type blockingGenerationTestGenerator struct{}

func (blockingGenerationTestGenerator) Generate(context.Context, string) (<-chan conversationgeneration.Event, error) {
	return make(chan conversationgeneration.Event), nil
}

func TestGenerationHarnessRequiresExplicitLargerJudge(t *testing.T) {
	harness := NewGenerationDependencies(GenerationDependenciesConfig{
		Searcher:        generationTestRetriever{},
		AnswerGenerator: &generationTestGenerator{},
		JudgeGenerator:  &generationTestGenerator{},
		AnswerModel:     "answer",
		JudgeModel:      "judge",
	})
	_, err := harness.Run(context.Background(), &GoldenSet{SchemaVersion: 2, Queries: []GoldenQuery{{
		ID: "q", Query: "q", ExpectedAnswer: "a", RequiredClaims: []string{"c"}, Relevant: []RelevantChunk{{File: "doc"}},
	}}}, 1)
	if err == nil || !strings.Contains(err.Error(), "larger judge") {
		t.Fatalf("Run error = %v, want explicit larger-judge validation", err)
	}
}

func TestCollectGenerationPropagatesEventErrors(t *testing.T) {
	generator := &generationTestGenerator{err: errors.New("offline")}
	if _, err := collectGeneration(context.Background(), generator, "prompt"); err == nil || err.Error() != "offline" {
		t.Fatalf("collectGeneration error = %v, want offline", err)
	}
}

func TestParseJudgeScoresRejectsOutOfRange(t *testing.T) {
	if _, err := parseJudgeScores(`{"faithfulness":1.1,"answer_relevancy":0.8,"context_precision":0.7,"context_recall":0.6}`); err == nil {
		t.Fatal("parseJudgeScores accepted score outside [0,1]")
	}
}

func TestJudgeResponseSchemaBoundsAllScores(t *testing.T) {
	schema := JudgeResponseSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want object", schema["properties"])
	}
	for _, name := range []string{"faithfulness", "answer_relevancy", "context_precision", "context_recall"} {
		property, ok := properties[name].(map[string]any)
		if !ok || property["minimum"] != 0.0 || property["maximum"] != 1.0 {
			t.Fatalf("schema property %q = %#v, want [0,1] number", name, properties[name])
		}
	}
}
