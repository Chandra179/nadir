package chat

import (
	"fmt"
	"strings"

	"nadir/internal/retrieval/search"
)

// ContextStats describes the bounded context presented to a generator.
// Tokens are an estimate used for operational diagnostics, not a tokenizer
// contract.
type ContextStats struct {
	Tokens         int
	IncludedChunks int
	Truncated      bool
}

type contextBuild struct {
	text  string
	stats ContextStats
}

// buildPrompt assembles the answer-generation prompt: grounded-answer
// instructions plus the numbered, token-budgeted context. Use-case logic on
// purpose — the generator is a dumb transport and must not know how RAG
// prompts are shaped.
func buildPrompt(query string, chunks []search.Chunk, maxTokens int) string {
	ordered := lostInMiddleOrder(chunks)
	context := buildContext(ordered, maxTokens).text

	var sb strings.Builder
	sb.WriteString("You are a precise assistant. Answer the question using ONLY the context below.\n")
	sb.WriteString("If the answer is not in the context, say \"I don't know based on the provided context.\"\n")
	sb.WriteString("Keep the answer concise and state only facts or formulas directly supported by the context.\n")
	sb.WriteString("Cite sources inline as [1], [2], etc. when referencing specific context sections.\n\n")
	sb.WriteString("Context:\n")
	sb.WriteString(context)
	sb.WriteString("\n\nQuestion: ")
	sb.WriteString(query)
	sb.WriteString("\n\nAnswer:")
	return sb.String()
}

// BuildPrompt assembles the production answer prompt for evaluation and other
// local callers that need to exercise the same prompt contract as Chat.
func BuildPrompt(query string, chunks []search.Chunk, maxTokens int) string {
	return buildPrompt(query, chunks, maxTokens)
}

// lostInMiddleOrder interleaves chunks front/back so the highest-ranked
// ones land at the prompt's edges, where attention is strongest.
func lostInMiddleOrder(chunks []search.Chunk) []search.Chunk {
	if len(chunks) <= 2 {
		return chunks
	}
	result := make([]search.Chunk, len(chunks))
	front, back := 0, len(chunks)-1
	for i, c := range chunks {
		if i%2 == 0 {
			result[front] = c
			front++
		} else {
			result[back] = c
			back--
		}
	}
	return result
}

func buildContext(chunks []search.Chunk, maxTokens int) contextBuild {
	var sb strings.Builder
	used := 0
	included := 0
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		source := c.FilePath
		if c.Header != "" {
			source += " > " + c.Header
		}
		entry := fmt.Sprintf("[%d] (source: %s)\n%s\n\n", i+1, source, text)
		entryTokens := estimateTokens(entry)
		if used+entryTokens > maxTokens {
			remaining := maxTokens - used
			if remaining > 15 {
				truncated := truncateToTokens(entry, remaining)
				if truncated != "" {
					sb.WriteString(truncated)
					included++
				}
			}
			return contextBuild{text: sb.String(), stats: ContextStats{
				Tokens:         estimateTokens(sb.String()),
				IncludedChunks: included,
				Truncated:      true,
			}}
		}
		sb.WriteString(entry)
		used += entryTokens
		included++
	}
	return contextBuild{text: sb.String(), stats: ContextStats{
		Tokens:         estimateTokens(sb.String()),
		IncludedChunks: included,
	}}
}

// BuildContext returns the bounded, lost-in-the-middle-ordered context used by
// BuildPrompt. Evaluation uses it to present exactly the retrieved evidence
// to the judge model without duplicating Chat prompt assembly.
func BuildContext(chunks []search.Chunk, maxTokens int) string {
	return BuildContextWithStats(chunks, maxTokens).Text
}

// ContextBuild is the bounded, ordered context and its diagnostic metadata.
// The source text remains available to the caller because this function is
// used at the generation boundary; reports should persist only the metadata.
type ContextBuild struct {
	Text  string
	Stats ContextStats
}

// BuildContextWithStats returns the same ordered context as BuildContext and
// reports whether the token budget removed or partially included a chunk.
func BuildContextWithStats(chunks []search.Chunk, maxTokens int) ContextBuild {
	built := buildContext(lostInMiddleOrder(chunks), maxTokens)
	return ContextBuild{Text: built.text, Stats: built.stats}
}

// estimateTokens approximates token count from word count (~1.3 tokens per
// word for English BPE tokenizers).
func estimateTokens(s string) int {
	words := len(strings.Fields(s))
	return int(float64(words)*1.3) + 1
}

// truncateToTokens cuts s down to approximately maxTokens tokens at a word
// boundary and marks the cut with an ellipsis.
func truncateToTokens(s string, maxTokens int) string {
	words := strings.Fields(s)
	maxWords := int(float64(maxTokens) / 1.3)
	if maxWords >= len(words) {
		return s
	}
	if maxWords <= 0 {
		return ""
	}
	return strings.Join(words[:maxWords], " ") + "..."
}
