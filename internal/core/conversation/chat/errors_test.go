package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"nadir/internal/core/conversation/generation"
	"nadir/internal/core/conversation/history"
	"nadir/internal/core/retrieval/search"
)

func TestChatDiagnosticsAndFailuresKeepPrivateContentOut(t *testing.T) {
	const question = "private-question-canary"
	const rewritten = "private-rewrite-canary"
	const detail = "private-provider-detail-canary"
	providerErr := errors.New("ollama response: " + detail + " " + question)
	for _, name := range []string{"search", "generation start", "generation stream", "edit", "rewrite failure", "rewrite success", "history list", "history create", "history append", "capacity"} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			h := &fakeHistory{}
			s := &fakeSearcher{chunks: []search.Chunk{{Text: "Ordinary evidence.", FilePath: "note.md"}}}
			g := &fakeGenerator{tokens: []string{"Supported answer."}}
			cfg := DependenciesConfig{Searcher: s, Generator: g, History: h,
				Log: slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
			req := Request{Query: question, TopK: 5}
			wantFailure := false
			switch name {
			case "search":
				s.err, wantFailure = providerErr, true
			case "generation start":
				g.err, req.Generate, wantFailure = providerErr, true, true
			case "generation stream":
				g.events = []generation.Event{{Kind: generation.EventError, Err: providerErr}}
				req.Generate, wantFailure = true, true
			case "edit":
				h.truncateErr = providerErr
				req.Edit, req.SessionID, req.EditSequence, wantFailure = true, "session-1", 1, true
			case "rewrite failure", "rewrite success", "history list":
				h.priorTurns = []history.Turn{{Query: "Earlier topic", Answer: "Earlier answer"}}
				req.SessionID = "session-1"
				rw := &fakeRewriter{rewritten: rewritten}
				cfg.Rewriter = rw
				if name == "rewrite failure" {
					rw.err = providerErr
				} else if name == "history list" {
					h.listErr = providerErr
				}
			case "history create":
				h.createErr = providerErr
			case "history append":
				h.appendErr = providerErr
			case "capacity":
				cfg.MaxRetainedTurns = 1
				req.Generate, wantFailure = true, true
			}
			d := NewDependencies(cfg)
			if name == "capacity" {
				d.broker.create("already-active")
			}
			turn := d.StartTurn(context.Background(), req)
			publicFailure := turn.Error + turn.GenerateError
			if turn.Streaming {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				events, detach, ok := d.Subscribe(ctx, turn.ID, 0)
				if !ok {
					t.Fatal("stream is unavailable")
				}
				defer detach()
				for ev := range events {
					if ev.Kind == EventError {
						publicFailure += ev.Text
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := d.Drain(ctx); err != nil {
				t.Fatal(err)
			}
			if wantFailure && publicFailure == "" {
				t.Fatal("failure was not explained to the user")
			}
			savedTurns := h.turns()
			if wantFailure && name != "edit" {
				if len(savedTurns) != 1 || savedTurns[0].Error+savedTurns[0].GenerateError != publicFailure {
					t.Fatal("saved failure differs from the live failure")
				}
				if savedTurns[0].Query != question {
					t.Fatal("diagnostic privacy must preserve the actual saved question")
				}
			}
			for _, saved := range savedTurns {
				publicFailure += saved.Error + saved.GenerateError
			}
			for _, private := range []string{question, rewritten, detail} {
				if strings.Contains(publicFailure, private) || strings.Contains(logs.String(), private) {
					t.Fatalf("private content %q escaped through errors or diagnostics", private)
				}
			}
			if logs.Len() == 0 || !strings.Contains(logs.String(), "operation_id") {
				t.Fatal("diagnostic correlation was lost")
			}
		})
	}
}

func TestChatKnownFailuresKeepUsefulGuidance(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"length", fmt.Errorf("retrieval: %w", search.ErrQueryTooLong), "Shorten it"},
		{"canceled", fmt.Errorf("provider: %w", context.Canceled), "was canceled"},
		{"deadline", fmt.Errorf("provider: %w", context.DeadlineExceeded), "timed out"},
		{"network timeout", &net.DNSError{Err: "private-network-canary", IsTimeout: true}, "timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDependencies(DependenciesConfig{Searcher: &fakeSearcher{err: tc.err}})
			turn := d.StartTurn(context.Background(), Request{Query: "Question", TopK: 5})
			if !strings.Contains(turn.Error, tc.want) || strings.Contains(turn.Error, "private-network-canary") {
				t.Fatalf("message lost safe actionable guidance: %q", turn.Error)
			}
		})
	}
}

func TestChatPromptBudgetFailureExplainsRecovery(t *testing.T) {
	gen := &fakeGenerator{}
	d := NewDependencies(DependenciesConfig{
		Searcher:  &fakeSearcher{chunks: []search.Chunk{{Text: "Evidence", FilePath: "note.md"}}},
		Generator: gen, ContextWindowTokens: 64, ReservedOutputTokens: 64,
	})
	turn := d.StartTurn(context.Background(), Request{Query: "Question", TopK: 5, Generate: true})
	if !strings.Contains(turn.GenerateError, "Shorten the question or start a new conversation") || gen.received() != "" {
		t.Fatalf("budget failure must explain recovery before calling the provider: %+v", turn)
	}
}
