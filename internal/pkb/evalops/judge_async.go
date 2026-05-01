package evalops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ContextJudge scores a chunk for relevance to a query (0–1).
type ContextJudge interface {
	ScoreContext(ctx context.Context, query string, text string) (float64, error)
}

// LLMContextJudge calls an OpenAI-compatible chat endpoint.
type LLMContextJudge struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

func NewLLMContextJudge(baseURL, model, apiKey string) *LLMContextJudge {
	return &LLMContextJudge{
		baseURL:    strings.TrimRight(baseURL, "/"),
		model:      model,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

const scorePrompt = `You are a relevance judge.

Query: %s

Passage:
%s

Rate relevance. Answer one word: LOW, MEDIUM, or HIGH.`

type judgeReq struct {
	Model    string        `json:"model"`
	Messages []judgeMsg    `json:"messages"`
}

type judgeMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type judgeResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (j *LLMContextJudge) ScoreContext(ctx context.Context, query, text string) (float64, error) {
	body := judgeReq{
		Model:    j.model,
		Messages: []judgeMsg{{Role: "user", Content: fmt.Sprintf(scorePrompt, query, text)}},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if j.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.apiKey)
	}
	resp, err := j.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("judge HTTP %d", resp.StatusCode)
	}
	var jr judgeResp
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		return 0, err
	}
	if len(jr.Choices) == 0 {
		return 0, fmt.Errorf("empty choices")
	}
	switch strings.TrimSpace(strings.ToUpper(jr.Choices[0].Message.Content)) {
	case "HIGH":
		return 1.0, nil
	case "MEDIUM":
		return 0.5, nil
	default:
		return 0.25, nil
	}
}
