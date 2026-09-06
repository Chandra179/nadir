// Package generator streams answers from an Ollama chat model. It owns the
// NDJSON wire format only: callers hand in a final prompt and receive typed
// events (Token / Error / Done) on a channel that closes when the stream
// ends.
package generator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatChunk struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
}

// Generate dials Ollama and returns a live event channel, fed by a
// goroutine owned by the generator. The channel closes after Done or Error;
// cancelling ctx stops generation at the next event boundary. The caller
// must drain or cancel to avoid pinning the feed goroutine.
func (g *dependencies) Generate(ctx context.Context, prompt string) (<-chan Event, error) {
	body, _ := json.Marshal(ollamaChatRequest{
		Model:    g.model,
		Messages: []ollamaMessage{{Role: "user", Content: prompt}},
		Stream:   true,
	})

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
		resp.Body.Close()
		return nil, fmt.Errorf("generator: status %d", resp.StatusCode)
	}

	events := make(chan Event, 16)
	go feed(ctx, resp.Body, events)
	return events, nil
}

// feed parses the Ollama NDJSON stream into events until EOF, error, or a
// cancelled context. It owns body and closes it.
func feed(ctx context.Context, body io.ReadCloser, events chan<- Event) {
	defer close(events)
	defer body.Close()

	reader := &ollamaTokenReader{body: body, scanner: bufio.NewScanner(body)}
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if !emit(ctx, events, TokenEvent{Text: string(buf[:n])}) {
				return
			}
		}
		if err == io.EOF {
			emit(ctx, events, DoneEvent{})
			return
		}
		if err != nil {
			emit(ctx, events, ErrorEvent{Err: err})
			return
		}
	}
}

// emit delivers one event, giving up if the consumer stops reading or the
// context is cancelled.
func emit(ctx context.Context, events chan<- Event, ev Event) bool {
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

type ollamaTokenReader struct {
	body    io.Closer
	scanner *bufio.Scanner
	buf     []byte
}

func (r *ollamaTokenReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if !r.scanner.Scan() {
			if err := r.scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		line := r.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunk ollamaChatChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		if chunk.Done {
			return 0, io.EOF
		}
		r.buf = []byte(chunk.Message.Content)
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *ollamaTokenReader) Close() error { return r.body.Close() }
