package chat

import (
	"testing"

	"nadir/internal/core/conversation/history"
)

func TestScopedConditionGuardDoesNotClaimGeneralAnswerability(t *testing.T) {
	subject := &history.Subject{FilePath: "transfer.md", Header: "Transfers > Durable transfers"}
	for _, tc := range []struct {
		name, query string
		citations   []Citation
	}{
		{"different topic", "What happens if it crashes?", []Citation{{FilePath: "other.md", Header: "Transfers > Immediate transfers", Text: "A crash loses work."}}},
		{"non-sibling section", "What happens if it crashes?", []Citation{{FilePath: "transfer.md", Header: "Transfers > Producers > Failures", Text: "A crash loses work."}}},
		{"named current subject", "What happens if immediate transfers crash?", []Citation{{FilePath: "transfer.md", Header: "Transfers > Immediate transfers", Text: "A crash loses work."}}},
		{"unknown phrasing", "Can it recover from a failure?", []Citation{{FilePath: "transfer.md", Header: "Transfers > Immediate transfers", Text: "A failure loses work."}}},
		{"shared ancestor covers event", "What happens if it crashes?", []Citation{{FilePath: "transfer.md", Header: "Transfers", Text: "After a crash, a durable transfer is retried."}, {FilePath: "transfer.md", Header: "Transfers > Immediate transfers", Text: "A crash loses work."}}},
		{"selected descendant covers event", "What happens if it crashes?", []Citation{{FilePath: "transfer.md", Header: "Transfers > Durable transfers > Recovery", Text: "After a crash, retry."}, {FilePath: "transfer.md", Header: "Transfers > Immediate transfers", Text: "A crash loses work."}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingScopedCondition(tc.query, subject, tc.citations); got != "" {
				t.Fatalf("scope guard became an unvalidated general answerability rule: %q", got)
			}
		})
	}
}
