package observability

import (
	"context"
	"errors"
	"testing"
)

func TestErrorLabelIsBoundedAndOperational(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "deadline", err: context.DeadlineExceeded, want: "deadline_exceeded"},
		{name: "generation", err: errors.New("generation failed"), want: "generation_error"},
		{name: "status", err: errors.New("adapter status 503"), want: "remote_status"},
		{name: "decode", err: errors.New("decode response"), want: "malformed_response"},
		{name: "shape", err: errors.New("score count mismatch"), want: "response_shape"},
		{name: "unknown", err: errors.New("something unexpected"), want: "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorLabel(tt.err); got != tt.want {
				t.Fatalf("ErrorLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
