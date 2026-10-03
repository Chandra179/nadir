// Package generator streams answers from an Ollama chat model. It owns the
// NDJSON wire format only: callers hand in a final prompt and receive typed
// events (Token / Error / Done) on a channel that closes when the stream
// ends.
package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	conversationgeneration "nadir/internal/core/conversation/generation"
)

type ollamaChatRequest struct {
	Model     string          `json:"model"`
	Messages  []ollamaMessage `json:"messages"`
	Stream    bool            `json:"stream"`
	KeepAlive string          `json:"keep_alive,omitempty"`
	Format    any             `json:"format,omitempty"`
	Options   map[string]any  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
}

type ollamaChatChunk struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// Generate dials Ollama and returns a live event channel, fed by a
// goroutine owned by the generator. The channel closes after Done or Error;
// cancelling ctx stops generation at the next event boundary. The caller
// must drain or cancel to avoid pinning the feed goroutine.
func (g *dependencies) Generate(ctx context.Context, prompt string) (<-chan conversationgeneration.Event, error) {
	body, err := json.Marshal(ollamaChatRequest{
		Model:     g.model,
		Messages:  []ollamaMessage{{Role: "user", Content: prompt}},
		Stream:    true,
		KeepAlive: g.keepAlive,
		Format:    g.format,
		Options:   g.options,
	})
	if err != nil {
		return nil, fmt.Errorf("generator encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.addr+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("generator build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("generator request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("generator: status %d", resp.StatusCode)
	}

	events := make(chan conversationgeneration.Event, 16)
	go feed(ctx, resp.Body, events)
	return events, nil
}

// feed parses the Ollama NDJSON stream into events until EOF, error, or a
// cancelled context. It owns body and closes it.
func feed(ctx context.Context, body io.ReadCloser, events chan<- conversationgeneration.Event) {
	defer func() { _ = body.Close() }()
	defer close(events)

	decoder := json.NewDecoder(body)
	hasAnswer := false
	finish := func() {
		if !hasAnswer {
			emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventError, Err: fmt.Errorf("generator stream ended without answer content; increase the generation budget or disable model thinking")})
		} else {
			emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventDone})
		}
	}
	for {
		var chunk ollamaChatChunk
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				finish()
			} else {
				emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventError, Err: fmt.Errorf("generator stream decode: %w", err)})
			}
			return
		}
		if chunk.Message.Content != "" {
			if !emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventToken, Text: chunk.Message.Content}) {
				return
			}
			hasAnswer = true
		}
		if chunk.Done {
			finish()
			return
		}
		if chunk.Message.Content == "" && chunk.Message.Thinking == "" {
			emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventError, Err: fmt.Errorf("generator stream response missing message.content")})
			return
		}
		// Reasoning is a separate wire field, never answer text. Continue
		// draining it under the same cancellation and request timeout.

	}
}

// emit delivers one event, giving up if the consumer stops reading or the
// context is cancelled.
func emit(ctx context.Context, events chan<- conversationgeneration.Event, ev conversationgeneration.Event) bool {
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
