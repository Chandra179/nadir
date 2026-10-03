package chat

import (
	"errors"

	"nadir/internal/core/observability"
	"nadir/internal/core/retrieval/search"
)

// failureMessage is safe to put in a live turn, stream event, or saved history.
// Provider error strings can contain endpoints, credentials or source content;
// only known conditions determine the user-facing explanation.
func failureMessage(action string, err error) string {
	if errors.Is(err, search.ErrQueryTooLong) {
		return "Question exceeds the configured length limit. Shorten it and try again."
	}
	switch observability.ErrorLabel(err) {
	case "canceled":
		return action + " was canceled. Please retry."
	case "deadline_exceeded", "timeout":
		return action + " timed out. Please retry when the local services are ready."
	default:
		return action + " failed. Please retry; if it continues, check the local service logs."
	}
}
