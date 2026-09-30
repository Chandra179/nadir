package indexing

import "errors"

// PublicationError reports a failure after a Document mutation became visible.
// Cleanup can be retried, but callers must already invalidate derived results.
// Persistence Adapters use this domain-owned outcome without exposing their
// storage protocol to indexing.
type PublicationError struct {
	Err error
}

func (e *PublicationError) Error() string { return e.Err.Error() }
func (e *PublicationError) Unwrap() error { return e.Err }

// WasPublished distinguishes a visible mutation with unfinished cleanup from
// a failure that left the previous corpus unchanged. It survives wrapping and
// joining errors at the indexing and transport boundaries.
func WasPublished(err error) bool {
	var published *PublicationError
	return errors.As(err, &published)
}
