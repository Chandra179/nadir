// Package generator streams answers from a Cloudflare Workers AI chat model.
// It owns the OpenAI-compatible SSE wire format only: callers hand in a final
// prompt and receive typed events (Token / Error / Done) on a channel that
// closes when the stream ends.
package generator

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	conversationgeneration "nadir/internal/core/conversation/generation"
	"nadir/internal/core/observability"
)

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// streamChunk is one SSE data payload. Reasoning deltas and usage-only chunks
// carry no answer content and decode to empty values here: reasoning is never
// answer text.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Generate calls Workers AI and returns a live event channel, fed by a
// goroutine owned by the generator. The channel closes after Done or Error;
// cancelling ctx stops generation at the next event boundary. The caller must
// drain or cancel to avoid pinning the feed goroutine.
func (g *dependencies) Generate(ctx context.Context, prompt string) (<-chan conversationgeneration.Event, error) {
	start := time.Now()
	resp, err := g.client.Post(ctx, "/chat/completions", chatRequest{
		Model:     g.model,
		Messages:  []chatMessage{{Role: "user", Content: prompt}},
		Stream:    true,
		MaxTokens: g.maxOutputTokens,
	}, map[string]string{"Accept": "text/event-stream"})
	if err != nil {
		return nil, fmt.Errorf("generator: %w", err)
	}

	events := make(chan conversationgeneration.Event, 16)
	go feed(ctx, resp.Body, events, g.telemetry, start)
	return events, nil
}

// feed parses the SSE stream into events until [DONE], a finish reason, an
// error, or a cancelled context. It owns body and closes it.
func feed(ctx context.Context, body io.ReadCloser, events chan<- conversationgeneration.Event, telemetry *observability.Recorder, start time.Time) {
	defer func() { _ = body.Close() }()
	defer close(events)

	outcome := "error"
	defer func() {
		if telemetry != nil {
			telemetry.Record("workersai.generate.total", outcome, time.Since(start))
		}
	}()

	fail := func(err error) {
		emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventError, Err: err})
	}

	reader := bufio.NewReader(body)
	hasAnswer, finished := false, false
	for !finished {
		line, readErr := reader.ReadString('\n')
		if payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			payload = strings.TrimSpace(payload)
			if payload == "[DONE]" {
				finished = true
				break
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				fail(fmt.Errorf("generator stream decode: %w", err))
				return
			}
			if chunk.Error != nil {
				fail(fmt.Errorf("generator stream error: %s", chunk.Error.Message))
				return
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					if !emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventToken, Text: choice.Delta.Content}) {
						return
					}
					hasAnswer = true
				}
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					finished = true
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			fail(fmt.Errorf("generator stream read: %w", readErr))
			return
		}
	}

	switch {
	case !finished:
		fail(fmt.Errorf("generator stream ended before completion"))
	case !hasAnswer:
		fail(fmt.Errorf("generator stream ended without answer content; increase the generation budget or choose a non-reasoning model"))
	default:
		outcome = "success"
		emit(ctx, events, conversationgeneration.Event{Kind: conversationgeneration.EventDone})
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
