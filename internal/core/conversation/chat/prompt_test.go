package chat

import (
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestBuildContextIncludesSectionHeaders(t *testing.T) {
	got := BuildContextWithStats([]search.Chunk{{
		FilePath: "linear-algebra.md",
		Header:   "Special Matrices",
		Text:     "Identity, diagonal, symmetric, and orthogonal matrices.",
	}}, 100).Text

	if !strings.Contains(got, "source: linear-algebra.md > Special Matrices") {
		t.Fatalf("context = %q, want section header in source citation", got)
	}
	prompt := BuildPromptWithBudget("what are special matrices", []search.Chunk{{
		FilePath: "linear-algebra.md",
		Header:   "Special Matrices",
		Text:     "Identity, diagonal, symmetric, and orthogonal matrices.",
	}}, PromptBudget{MaxContextTokens: 100}).Prompt
	if !strings.Contains(prompt, "source: linear-algebra.md > Special Matrices") {
		t.Fatalf("prompt = %q, want section header in generated context", prompt)
	}
}

func TestFlattenedTablePresentationRetainsRowsEmptyCellsAndSourceIdentity(t *testing.T) {
	const table = "| Service | Policy | Note | |---|---|---| | Tracking | Available | fast | | Matching | Consistent | |"
	chunk := search.Chunk{FilePath: "design.md", SectionPath: "Design > Policies", SourceSHA: "version", LineStart: 20, Text: table}
	built := BuildPromptWithBudget("Which service uses each policy?", []search.Chunk{chunk}, PromptBudget{MaxContextTokens: 1000})
	if len(built.Context.Citations) != 1 {
		t.Fatalf("table lost evidence: %+v", built)
	}
	c := built.Context.Citations[0]
	want := "| Service | Policy | Note |\n| --- | --- | --- |\n| Tracking | Available | fast |\n| Matching | Consistent |  |"
	if c.Text != want || !strings.Contains(built.Prompt, want) || chunk.Text != table || c.SourceSHA != chunk.SourceSHA || c.LineStart != chunk.LineStart {
		t.Fatalf("table row/value or evidence provenance changed: %+v", c)
	}
	for _, text := range []string{"if a || b { return }", "| a | b | |---|---| | row |", "| | |", "Plain text | with pipes |"} {
		if got := restoreTableRows(text); got != text {
			t.Fatalf("non-table fragment changed: %q to %q", text, got)
		}
	}
}

func TestCitationLabelRetainsTheParentOfAnAmbiguousHeading(t *testing.T) {
	chunk := search.Chunk{FilePath: "methods.md", Header: "Properties", SectionPath: "Methods > First method > Properties", Text: "Requires a derivative.", LineStart: 7}
	built := BuildContextWithStats([]search.Chunk{chunk}, 200)
	if len(built.Citations) != 1 || built.Citations[0].Header != chunk.SectionPath || built.Citations[0].Text != chunk.Text || built.Citations[0].LineStart != 7 {
		t.Fatalf("citation lost its subject, evidence or source location: %+v", built)
	}
	if chunk.Header != "Properties" {
		t.Fatalf("citation presentation changed the filterable leaf: %+v", chunk)
	}
}

func TestSourceFootnotesCannotMasqueradeAsApplicationCitationIDs(t *testing.T) {
	chunk := search.Chunk{Text: "singleflight[^7] coalesces calls. The value [7] is an array.", FilePath: "api.md", SourceSHA: "version", LineStart: 9}
	built := BuildPromptWithBudget("How are calls coalesced?", []search.Chunk{chunk}, PromptBudget{MaxContextTokens: 300})
	if len(built.Context.Citations) != 1 || strings.Contains(built.Context.Citations[0].Text, "[^7]") || !strings.Contains(built.Context.Citations[0].Text, "document footnote)") || strings.Contains(built.Context.Citations[0].Text, "footnote 7") || !strings.Contains(built.Context.Citations[0].Text, "value [7]") {
		t.Fatalf("source-local numbering is ambiguous or an ordinary value changed: %+v", built.Context)
	}
	if !strings.Contains(built.Prompt, citationEntry(built.Context.Citations[0])) || chunk.Text != "singleflight[^7] coalesces calls. The value [7] is an array." || built.Context.Citations[0].LineStart != 9 || built.Context.Citations[0].SourceSHA != "version" {
		t.Fatalf("presented snapshot differs or stored source identity changed: %+v", built)
	}
}

func TestBuildContextWithStatsReportsTruncation(t *testing.T) {
	got := BuildContextWithStats([]search.Chunk{{
		FilePath: "calculus.md",
		Header:   "Power Rule",
		Text:     "The derivative of a power function is n times x to the n minus one.",
	}}, 34)

	if !got.Stats.Truncated {
		t.Fatal("context stats reported no truncation for an undersized budget")
	}
	if got.Stats.IncludedChunks != 1 {
		t.Fatalf("included chunks = %d, want one partially included chunk", got.Stats.IncludedChunks)
	}
}

func TestContextAdmissionPreservesRankBeforePresentation(t *testing.T) {
	var chunks []search.Chunk
	for _, rank := range []string{"rank1", "rank2", "rank3", "rank4", "rank5"} {
		chunks = append(chunks, search.Chunk{FilePath: rank + ".md", Text: strings.Repeat(rank+" ", 20)})
	}
	// This reproduced rank #3 consuming the budget while rank #2, reordered
	// to the prompt's far edge, was entirely excluded.
	built := BuildContextWithStats(chunks, 110)
	if !strings.Contains(built.Text, "rank2") || strings.Contains(built.Text, "rank3") {
		t.Fatalf("admission skipped a higher-ranked chunk: %q", built.Text)
	}
	for i, citation := range built.Citations {
		if citation.Number != i+1 || citation.RetrievalRank != i+1 || citation.FilePath != chunks[i].FilePath {
			t.Fatalf("citation map does not match ranking: %+v", built.Citations)
		}
	}
	if built.Stats.Tokens > 110 {
		t.Fatalf("context exceeded estimate budget: %+v", built.Stats)
	}
}

func TestCitationPresentationMatchesRankAndSourceNumbers(t *testing.T) {
	chunks := []search.Chunk{
		{FilePath: "first.md", Text: "First", LineStart: 3, ChunkIndex: 7},
		{FilePath: "second.md", Text: "Second", LineStart: 9, ChunkIndex: 2},
		{FilePath: "third.md", Text: "Third", LineStart: 20, ChunkIndex: 4},
	}
	built := BuildPromptWithBudget("which", chunks, PromptBudget{MaxContextTokens: 500})
	if strings.Index(built.Context.Text, "[1]") > strings.Index(built.Context.Text, "[2]") || strings.Index(built.Context.Text, "[2]") > strings.Index(built.Context.Text, "[3]") {
		t.Fatalf("source presentation does not match citation order: %q", built.Context.Text)
	}
	if !strings.Contains(built.Context.Text, "[2] (source: second.md (line 9, chunk 2))") {
		t.Fatalf("citation #2 lost its provenance: %q", built.Context.Text)
	}
	for _, citation := range built.Context.Citations {
		if !strings.Contains(built.Prompt, citationEntry(citation)) {
			t.Fatalf("prompt differs from admitted evidence: %+v", citation)
		}
	}
}

func TestPromptBudgetReservesQuestionInstructionsAndAnswer(t *testing.T) {
	query := strings.Repeat("what exactly does this equation mean? ", 12)
	chunks := []search.Chunk{{FilePath: "formula.md", Text: strings.Repeat(`x_{n+1}=(x_n*f(x_{n-1})-x_{n-1}*f(x_n))/(f(x_{n-1})-f(x_n));`, 80)}}
	built := BuildPromptWithBudget(query, chunks, PromptBudget{
		MaxContextTokens: 2800, ContextWindowTokens: 2048, ReservedOutputTokens: 512,
	})
	if built.Err != nil {
		t.Fatal(built.Err)
	}
	if estimateTokens(built.Prompt)+512+64 > 2048 {
		t.Fatalf("complete request exceeds model window: %+v", built.Context.Stats)
	}
	if !built.Context.Stats.Truncated || len(built.Context.Citations) != 1 || !built.Context.Citations[0].Truncated {
		t.Fatalf("bounded evidence was not marked: %+v", built.Context)
	}
	if !strings.Contains(built.Prompt, "(source: formula.md)") {
		t.Fatal("partial admission truncated the source label")
	}
}

func TestPromptRejectsQuestionThatExhaustsWindow(t *testing.T) {
	built := BuildPromptWithBudget(strings.Repeat("dense", 1000), nil, PromptBudget{
		MaxContextTokens: 2800, ContextWindowTokens: 1024, ReservedOutputTokens: 512,
	})
	if built.Err == nil || built.Prompt != "" {
		t.Fatalf("oversized question should not be sent to the model: %+v", built)
	}
}

func TestTokenEstimatorCountsDenseTextAndUnicode(t *testing.T) {
	for _, text := range []string{strings.Repeat("abc", 100), strings.Repeat("x_i^2+", 100), strings.Repeat("方程", 100)} {
		if estimateTokens(text) < (len(text)+2)/3 {
			t.Fatalf("dense text underestimated: %d bytes estimated at %d tokens", len(text), estimateTokens(text))
		}
		truncated := truncateToTokens(text, 30)
		if estimateTokens(truncated) > 30 {
			t.Fatalf("truncated estimate exceeded budget: %q", truncated)
		}
	}
}

func TestContextDoesNotAdmitSourceLabelWithoutEvidence(t *testing.T) {
	built := BuildContextWithStats([]search.Chunk{{FilePath: "long-source-name.md", Text: "answer"}}, 4)
	if built.Text != "" || len(built.Citations) != 0 || !built.Stats.Truncated {
		t.Fatalf("tiny budget produced a false citation: %+v", built)
	}
}

func TestBuildPromptShapesAnswersAndAbstention(t *testing.T) {
	prompt := BuildPromptWithBudget("why add C to an indefinite integral", []search.Chunk{{
		FilePath: "calculus.md",
		Header:   "Antiderivatives",
		Text:     "An antiderivative collects a constant of integration C.",
	}}, PromptBudget{MaxContextTokens: 1000}).Prompt
	for _, clause := range []string{
		`"what is" question gets the value or formula itself`,
		`"why" or "how" question gets the answer plus one to three short sentences`,
		"say so in one sentence and stop",
		"Never add facts that are not in the context",
	} {
		if !strings.Contains(prompt, clause) {
			t.Fatalf("prompt missing shaping/abstention clause %q", clause)
		}
	}
}
