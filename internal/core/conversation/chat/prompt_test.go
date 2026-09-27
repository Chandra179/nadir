package chat

import (
	"strings"
	"testing"

	"nadir/internal/core/retrieval/search"
)

func TestBuildContextIncludesSectionHeaders(t *testing.T) {
	got := BuildContext([]search.Chunk{{
		FilePath: "linear-algebra.md",
		Header:   "Special Matrices",
		Text:     "Identity, diagonal, symmetric, and orthogonal matrices.",
	}}, 100)

	if !strings.Contains(got, "source: linear-algebra.md > Special Matrices") {
		t.Fatalf("context = %q, want section header in source citation", got)
	}
	prompt := BuildPrompt("what are special matrices", []search.Chunk{{
		FilePath: "linear-algebra.md",
		Header:   "Special Matrices",
		Text:     "Identity, diagonal, symmetric, and orthogonal matrices.",
	}}, 100)
	if !strings.Contains(prompt, "source: linear-algebra.md > Special Matrices") {
		t.Fatalf("prompt = %q, want section header in generated context", prompt)
	}
}

func TestBuildContextWithStatsReportsTruncation(t *testing.T) {
	got := BuildContextWithStats([]search.Chunk{{
		FilePath: "calculus.md",
		Header:   "Power Rule",
		Text:     "The derivative of a power function is n times x to the n minus one.",
	}}, 16)

	if !got.Stats.Truncated {
		t.Fatal("context stats reported no truncation for an undersized budget")
	}
	if got.Stats.IncludedChunks != 1 {
		t.Fatalf("included chunks = %d, want one partially included chunk", got.Stats.IncludedChunks)
	}
}

func TestBuildPromptShapesAnswersAndAbstention(t *testing.T) {
	prompt := BuildPrompt("why add C to an indefinite integral", []search.Chunk{{
		FilePath: "calculus.md",
		Header:   "Antiderivatives",
		Text:     "An antiderivative collects a constant of integration C.",
	}}, 1000)
	for _, clause := range []string{
		`"what is" question gets the value or formula itself`,
		`"why" or "how" question gets the answer plus one to three short sentences`,
		"labeled as related background",
	} {
		if !strings.Contains(prompt, clause) {
			t.Fatalf("prompt missing shaping/abstention clause %q", clause)
		}
	}
}
