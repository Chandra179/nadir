package chat

import (
	"fmt"
	"strings"

	"nadir/internal/store"
)

// buildPrompt assembles the answer-generation prompt: grounded-answer
// instructions plus the numbered, token-budgeted context. Use-case logic on
// purpose — the generator is a dumb transport and must not know how RAG
// prompts are shaped.
func buildPrompt(query string, chunks []store.ScoredChunk, maxTokens int) string {
	ordered := lostInMiddleOrder(chunks)
	context := buildContext(ordered, maxTokens)

	var sb strings.Builder
	sb.WriteString("You are a precise assistant. Answer the question using ONLY the context below.\n")
	sb.WriteString("If the answer is not in the context, say \"I don't know based on the provided context.\"\n")
	sb.WriteString("Cite sources inline as [1], [2], etc. when referencing specific context sections.\n\n")
	sb.WriteString("Context:\n")
	sb.WriteString(context)
	sb.WriteString("\n\nQuestion: ")
	sb.WriteString(query)
	sb.WriteString("\n\nAnswer:")
	return sb.String()
}

// lostInMiddleOrder interleaves chunks front/back so the highest-ranked
// ones land at the prompt's edges, where attention is strongest.
func lostInMiddleOrder(chunks []store.ScoredChunk) []store.ScoredChunk {
	if len(chunks) <= 2 {
		return chunks
	}
	result := make([]store.ScoredChunk, len(chunks))
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

func buildContext(chunks []store.ScoredChunk, maxTokens int) string {
	var sb strings.Builder
	used := 0
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		entry := fmt.Sprintf("[%d] (source: %s)\n%s\n\n", i+1, c.FilePath, text)
		entryTokens := estimateTokens(entry)
		if used+entryTokens > maxTokens {
			remaining := maxTokens - used
			if remaining > 15 {
				truncated := truncateToTokens(entry, remaining)
				sb.WriteString(truncated)
			}
			break
		}
		sb.WriteString(entry)
		used += entryTokens
	}
	return sb.String()
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
