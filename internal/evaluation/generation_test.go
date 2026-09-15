package evaluation

import (
	"context"
	"errors"
	"strings"
	"testing"

	conversationgeneration "nadir/internal/conversation/generation"
	"nadir/internal/retrieval/search"
)

type generationTestRetriever struct {
	chunks []search.Chunk
}

func (r generationTestRetriever) Query(_ context.Context, _ search.Request) (search.Result, error) {
	return search.Result{Chunks: r.chunks}, nil
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
