package pkb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// LLMJudge calls an OpenAI-compatible chat completion endpoint to judge relevance.
// Swap backend by changing BaseURL + Model (Ollama, OpenAI, Anthropic-compatible proxy, etc.).
type LLMJudge struct {
	BaseURL    string // e.g. "http://localhost:11434/v1" for Ollama, "https://api.openai.com/v1" for OpenAI
	Model      string // e.g. "llama3", "gpt-4o-mini"
	APIKey     string // empty for Ollama
	HTTPClient *http.Client
}

func NewLLMJudge(baseURL, model, apiKey string) *LLMJudge {
	return &LLMJudge{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Model:      model,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

const judgePrompt = `You are a relevance judge for a personal knowledge base search system.

Query: %s

Passage:
%s

Is this passage relevant to the query? Answer with a single word: YES or NO.`

func (j *LLMJudge) IsRelevant(ctx context.Context, query string, chunk ScoredChunk) (bool, error) {
	prompt := fmt.Sprintf(judgePrompt, query, chunk.Text)

	reqBody := chatRequest{
		Model: j.Model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if j.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.APIKey)
	}

	resp, err := j.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("LLM judge HTTP %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return false, fmt.Errorf("empty choices from LLM judge")
	}

	answer := strings.TrimSpace(strings.ToUpper(chatResp.Choices[0].Message.Content))
	return strings.HasPrefix(answer, "YES"), nil
}
