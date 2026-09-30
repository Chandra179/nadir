package chat

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"nadir/internal/core/retrieval/search"
)

const promptInstructions = "You are a precise assistant. Answer the question using ONLY the context below.\n" +
	"Match the answer to the question: a lookup or \"what is\" question gets the value or formula itself; a \"why\" or \"how\" question gets the answer plus one to three short sentences of explanation from the context.\n" +
	"Do not pad the answer beyond what the question asks.\n" +
	"If the context does not contain the answer, say so in one sentence and stop. Never add facts that are not in the context.\n" +
	"Cite sources inline as [1], [2], etc. when referencing specific context sections.\n\nContext:\n"

// PromptBudget bounds evidence and, when configured, the complete model
// request. ContextWindowTokens includes the reserved answer and a small
// allowance for the provider's chat template. Counts are conservative estimates.
type PromptBudget struct {
	MaxContextTokens     int
	ContextWindowTokens  int
	ReservedOutputTokens int
}

// ContextStats describes the actual evidence admitted to the prompt.
type ContextStats struct {
	Tokens               int
	IncludedChunks       int
	Truncated            bool
	BudgetTokens         int
	PromptTokens         int
	ReservedOutputTokens int
}

// Citation identifies one admitted piece of evidence. Number is assigned in
// retrieval order before arranging the prompt, so it never refers to a
// different source merely because the prompt order changed. Text snapshots
// exactly the evidence presented to the model, including any truncation.
type Citation struct {
	Number        int
	RetrievalRank int
	FilePath      string
	Header        string
	LineStart     int
	ChunkIndex    int
	SourceSHA     string
	Text          string
	Truncated     bool
}

// ContextBuild is bounded evidence with an explicit citation map.
type ContextBuild struct {
	Text      string
	Stats     ContextStats
	Citations []Citation
}

// PromptBuild is one shared assembly result for generation, judging and
// source display. Err is set when the question alone exhausts the model budget.
type PromptBuild struct {
	Prompt  string
	Context ContextBuild
	Err     error
}

func promptSuffix(query string) string { return "\n\nQuestion: " + query + "\n\nAnswer:" }

// BuildPromptWithBudget is the sole generation prompt API: it selects
// evidence by retrieval rank, then arranges only the admitted chunks at the
// prompt edges. Lower-ranked evidence never takes budget away from a
// higher-ranked chunk.
func BuildPromptWithBudget(query string, chunks []search.Chunk, budget PromptBudget) PromptBuild {
	contextTokens := max(0, budget.MaxContextTokens)
	reserved := max(0, budget.ReservedOutputTokens)
	suffix := promptSuffix(query)
	if budget.ContextWindowTokens > 0 {
		// Ollama adds a model-specific chat template outside the prompt.
		const templateAllowance = 64
		available := budget.ContextWindowTokens - reserved - templateAllowance - estimateTokens(promptInstructions+suffix)
		if available < 0 {
			return PromptBuild{Err: fmt.Errorf("question and reserved answer exceed the model context window (%d tokens)", budget.ContextWindowTokens)}
		}
		contextTokens = min(contextTokens, available)
	}
	context := BuildContextWithStats(chunks, contextTokens)
	prompt := promptInstructions + context.Text + suffix
	context.Stats.PromptTokens = estimateTokens(prompt)
	context.Stats.ReservedOutputTokens = reserved
	return PromptBuild{Prompt: prompt, Context: context}
}

// lostInMiddleOrder interleaves chunks front/back so the best two occupy the
// prompt edges. Admission must always precede this presentation transform.
func lostInMiddleOrder(chunks []search.Chunk) []search.Chunk {
	return edgeOrder(chunks)
}

func edgeOrder[T any](items []T) []T {
	result := make([]T, len(items))
	front, back := 0, len(items)-1
	for i, item := range items {
		if i%2 == 0 {
			result[front] = item
			front++
		} else {
			result[back] = item
			back--
		}
	}
	return result
}

func citationEntry(c Citation) string {
	source := c.FilePath
	if c.Header != "" {
		source += " > " + c.Header
	}
	if c.LineStart > 0 {
		source += fmt.Sprintf(" (line %d, chunk %d)", c.LineStart, c.ChunkIndex)
	}
	return fmt.Sprintf("[%d] (source: %s)\n%s\n\n", c.Number, source, c.Text)
}

// BuildContextWithStats selects a ranked prefix. The last admitted chunk may
// contain a text prefix; its source label is always complete. Citations stay
// in retrieval order even though Text is arranged for model attention.
func BuildContextWithStats(chunks []search.Chunk, maxTokens int) ContextBuild {
	built := ContextBuild{Stats: ContextStats{BudgetTokens: max(0, maxTokens)}}
	used := 0
	for i, chunk := range chunks {
		text := chunk.WindowText
		if text == "" {
			text = chunk.Text
		}
		citation := Citation{Number: i + 1, RetrievalRank: i + 1, FilePath: chunk.FilePath,
			Header: chunk.Header, LineStart: chunk.LineStart, ChunkIndex: chunk.ChunkIndex,
			SourceSHA: chunk.SourceSHA, Text: text}
		entryTokens := estimateTokens(citationEntry(citation))
		if used+entryTokens > maxTokens {
			built.Stats.Truncated = true
			remaining := maxTokens - used
			citation.Text = ""
			textBudget := remaining - estimateTokens(citationEntry(citation))
			citation.Text = truncateToTokens(text, textBudget)
			citation.Truncated = true
			if citation.Text != "" && estimateTokens(citationEntry(citation)) <= remaining {
				built.Citations = append(built.Citations, citation)
			}
			break
		}
		used += entryTokens
		built.Citations = append(built.Citations, citation)
	}
	var sb strings.Builder
	for _, citation := range edgeOrder(built.Citations) {
		sb.WriteString(citationEntry(citation))
	}
	built.Text = sb.String()
	built.Stats.Tokens = estimateTokens(built.Text)
	built.Stats.IncludedChunks = len(built.Citations)
	return built
}

func BuildContext(chunks []search.Chunk, maxTokens int) string {
	return BuildContextWithStats(chunks, maxTokens).Text
}

// EstimateTokens is the shared conservative prompt estimate for answer and
// judge model windows. It is independent of a provider-specific tokenizer.
func EstimateTokens(s string) int { return estimateTokens(s) }

// estimateTokens uses both character density and lexical boundaries. Word
// count alone badly undercounts equations, code, long identifiers and CJK.
// This is deliberately conservative and remains independent of any one
// generator tokenizer; the model window also reserves template headroom.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	tokens, run := 0, 0
	flush := func() {
		tokens += (run + 2) / 3
		run = 0
	}
	for _, r := range s {
		switch {
		case r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			run++
		case unicode.IsSpace(r):
			flush()
			if r == '\n' {
				tokens++
			}
		default:
			flush()
			tokens++
		}
	}
	flush()
	return max(tokens, (len(s)+2)/3)
}

// truncateToTokens retains original whitespace and rune boundaries; it never
// collapses formulas or code into a sequence of words.
func truncateToTokens(s string, maxTokens int) string {
	if estimateTokens(s) <= maxTokens {
		return s
	}
	const marker = "\n[truncated]"
	if maxTokens <= estimateTokens(marker) {
		return ""
	}
	runes := []rune(s)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if estimateTokens(string(runes[:mid])+marker) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if text := strings.TrimSpace(string(runes[:lo])); text != "" {
		return text + marker
	}
	return ""
}
