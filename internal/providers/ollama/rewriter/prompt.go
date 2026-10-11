package rewriter

import conversationrewriting "nadir/internal/core/conversation/rewriting"

// Prompt returns the rewrite instruction and the conversation prompt for a
// follow-up. Adapters for other chat models reuse the exact wording so the
// measured rewrite policy (ADR 0002/0035) does not fork per runtime.
func Prompt(turns []conversationrewriting.Turn, query string) (system, user string) {
	return systemPrompt, buildPrompt(turns, query)
}

// CleanQuery normalizes a model's rewrite output into a plain question.
func CleanQuery(output string) string { return cleanQuery(output) }
