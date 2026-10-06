package chat

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"nadir/internal/core/retrieval/search"
)

const promptInstructions = "You are a precise assistant. Answer the question using ONLY the context below.\n" +
	"Match the answer to the question: a lookup or \"what is\" question gets the value or formula itself; a \"why\" or \"how\" question gets the answer plus one to three short sentences of explanation from the context.\n" +
	"Do not pad the answer beyond what the question asks.\n" +
	"Answer every expressly requested part, including both sides of a comparison. Name the alternatives in your answer; a property of a third alternative is not an answer to the requested comparison. Preserve source conditions and restrictions.\n" +
	"For each factual sentence, cite the section containing that fact, not merely a related definition. A partial word or incomplete statement is not evidence for its missing part.\n" +
	"If the context does not contain the answer, say so in one sentence and stop. Never add facts that are not in the context.\n" +
	"Cite sources inline as [1], [2], etc. when referencing specific context sections.\n\nContext:\n"

var sourceFootnote = regexp.MustCompile(`\[\^(\d+)\]`)

// PromptBudget bounds evidence and, when configured, the complete model
// request. ContextWindowTokens includes the reserved answer and a small
// allowance for the provider's chat template. Counts are conservative estimates.
type PromptBudget struct {
	MaxContextTokens     int
	ContextWindowTokens  int
	ReservedOutputTokens int
	// ReferenceContext resolves conversation subjects but supplies no evidence.
	// Place it before the current question so the current intent stays last.
	ReferenceContext string
	// ResolvedSubject pins a selected alternative without excluding contrast
	// evidence. It is a reference label, not a source of factual claims.
	ResolvedSubject string
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
// retrieval order, which is also the prompt presentation order. Text snapshots
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

// PromptBuild is one shared assembly result for generation and
// source display. Err is set when the question alone exhausts the model budget.
type PromptBuild struct {
	Prompt  string
	Context ContextBuild
	Err     error
}

func promptSuffix(query, reference string) string {
	return reference + "\n\nQuestion: " + query +
		"\n\nAnswer:"
}

// BuildPromptWithBudget is the sole generation prompt API: it selects
// evidence by retrieval rank and presents that same order. Lower-ranked
// evidence never takes budget away from a
// higher-ranked chunk.
func BuildPromptWithBudget(query string, chunks []search.Chunk, budget PromptBudget) PromptBuild {
	contextTokens := max(0, budget.MaxContextTokens)
	reserved := max(0, budget.ReservedOutputTokens)
	reference := budget.ReferenceContext
	if budget.ResolvedSubject != "" {
		reference += "\nResolved conversation subject: " + truncateToTokens(budget.ResolvedSubject, 160) +
			"\nAnswer the current question for this subject. Evidence about another alternative does not establish this subject's behavior. If the evidence for this subject does not explain the requested event or condition, state that missing detail instead of inferring an outcome."
	}
	suffix := promptSuffix(query, reference)
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
// in retrieval order, matching their presentation in Text.
func BuildContextWithStats(chunks []search.Chunk, maxTokens int) ContextBuild {
	built := ContextBuild{Stats: ContextStats{BudgetTokens: max(0, maxTokens)}}
	used := 0
	for i, chunk := range chunks {
		text := chunk.WindowText
		if text == "" {
			text = chunk.Text
		}
		// Source-local footnote numbers must not become app source IDs. Stored
		// text and ordinary bracketed values remain intact; Citation.Text records
		// the exact normalized evidence shown to the model and reader.
		text = sourceFootnote.ReplaceAllString(text, " (document footnote)")
		text = restoreTableRows(text)
		heading := chunk.SectionPath
		if heading == "" {
			heading = chunk.Header
		}
		citation := Citation{Number: i + 1, RetrievalRank: i + 1, FilePath: chunk.FilePath,
			Header: heading, LineStart: chunk.LineStart, ChunkIndex: chunk.ChunkIndex,
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
	for _, citation := range built.Citations {
		sb.WriteString(citationEntry(citation))
	}
	built.Text = sb.String()
	built.Stats.Tokens = estimateTokens(built.Text)
	built.Stats.IncludedChunks = len(built.Citations)
	return built
}

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
