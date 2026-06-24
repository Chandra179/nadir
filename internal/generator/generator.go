package generator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"nadir/internal/store"
)

type Generator interface {
	Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (io.ReadCloser, error)
}

type OllamaGenerator struct {
	addr             string
	model            string
	maxContextTokens int
	client           *http.Client
}

func NewOllamaGenerator(addr, model string, maxContextTokens int) *OllamaGenerator {
	if maxContextTokens <= 0 {
		maxContextTokens = 2800
	}
	return &OllamaGenerator{
		addr:             addr,
		model:            model,
		maxContextTokens: maxContextTokens,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

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

func (g *OllamaGenerator) Generate(ctx context.Context, query string, chunks []store.ScoredChunk) (io.ReadCloser, error) {
	prompt := buildPrompt(query, chunks, g.maxContextTokens)
	log.Printf("[generator] RAG context passed to LLM:\n%s", prompt)

	body, _ := json.Marshal(ollamaChatRequest{
		Model: g.model,
		Messages: []ollamaMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true,
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

	return &ollamaTokenReader{body: resp.Body, scanner: bufio.NewScanner(resp.Body)}, nil
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
	const charsPerToken = 4
	budget := maxTokens * charsPerToken

	var sb strings.Builder
	for i, c := range chunks {
		text := c.WindowText
		if text == "" {
			text = c.Text
		}
		entry := fmt.Sprintf("[%d] (source: %s)\n%s\n\n", i+1, c.FilePath, text)
		if sb.Len()+len(entry) > budget {
			remaining := budget - sb.Len()
			if remaining > 60 {
				truncated := entry[:remaining-3] + "..."
				sb.WriteString(truncated)
			}
			break
		}
		sb.WriteString(entry)
	}
	return sb.String()
}
