// Package vectorize is a small client for Cloudflare Vectorize's REST API
// (/accounts/{id}/vectorize/v2/indexes/{index}). It owns request and response
// envelopes only. Vectorize applies writes asynchronously: an upserted vector
// can take a short time to appear in queries, and callers must not rely on
// reading their own write immediately.
//
// The request shapes (notably the NDJSON upsert body) were not verified
// against live Vectorize; they follow the API reference as mirrored by SDKs.
package vectorize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nadir/internal/providers/cloudflare/cfapi"
)

// maxDeleteBatch is the most ids sent to one delete_by_ids request.
const maxDeleteBatch = 1000

// Config groups the account, index and credential. APIToken is a secret: it is
// sent only as a bearer header and never logged or included in errors.
type Config struct {
	AccountID      string
	APIToken       string
	Index          string
	BaseURL        string // empty uses the Cloudflare default; tests point it at httptest
	RequestTimeout time.Duration
}

// Client runs operations against one Vectorize index.
type Client struct {
	api  *cfapi.Client
	base string
}

// Match is one query result. Score is the index metric's similarity.
type Match struct {
	ID    string
	Score float32
}

// IndexConfig is the dimension and distance metric an index was created with.
type IndexConfig struct {
	Dimensions int
	Metric     string
}

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Index) == "" {
		return nil, fmt.Errorf("vectorize index name is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	api, err := cfapi.New(cfapi.Config{AccountID: cfg.AccountID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL, RequestTimeout: timeout})
	if err != nil {
		return nil, err
	}
	return &Client{api: api, base: "/vectorize/v2/indexes/" + url.PathEscape(cfg.Index)}, nil
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (e envelope) err(what string) error {
	if e.Success {
		return nil
	}
	msg := "unsuccessful response"
	if len(e.Errors) > 0 && e.Errors[0].Message != "" {
		msg = e.Errors[0].Message
	}
	return fmt.Errorf("vectorize %s: %s", what, msg)
}

func (c *Client) send(ctx context.Context, what string, req cfapi.Request, out any) error {
	resp, err := c.api.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("vectorize %s: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("vectorize %s: decode response: %w", what, err)
	}
	return nil
}

// Describe returns the index's configured dimensions and metric.
func (c *Client) Describe(ctx context.Context) (IndexConfig, error) {
	var out struct {
		envelope
		Result struct {
			Config struct {
				Dimensions int    `json:"dimensions"`
				Metric     string `json:"metric"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := c.send(ctx, "describe", cfapi.Request{Method: http.MethodGet, Path: c.base}, &out); err != nil {
		return IndexConfig{}, err
	}
	if err := out.err("describe"); err != nil {
		return IndexConfig{}, err
	}
	return IndexConfig{Dimensions: out.Result.Config.Dimensions, Metric: out.Result.Config.Metric}, nil
}

// Query returns up to topK nearest vectors, best first, without values or
// metadata.
func (c *Client) Query(ctx context.Context, vector []float32, topK int) ([]Match, error) {
	body, err := json.Marshal(map[string]any{
		"vector": vector, "topK": topK, "returnValues": false, "returnMetadata": "none",
	})
	if err != nil {
		return nil, fmt.Errorf("vectorize query: encode request: %w", err)
	}
	var out struct {
		envelope
		Result struct {
			Matches []struct {
				ID    string  `json:"id"`
				Score float32 `json:"score"`
			} `json:"matches"`
		} `json:"result"`
	}
	if err := c.send(ctx, "query", cfapi.Request{Method: http.MethodPost, Path: c.base + "/query", ContentType: "application/json", Body: body}, &out); err != nil {
		return nil, err
	}
	if err := out.err("query"); err != nil {
		return nil, err
	}
	matches := make([]Match, len(out.Result.Matches))
	for i, m := range out.Result.Matches {
		matches[i] = Match{ID: m.ID, Score: m.Score}
	}
	return matches, nil
}

// Upsert inserts or replaces the vector with this id. The write is accepted
// immediately and applied asynchronously.
func (c *Client) Upsert(ctx context.Context, id string, vector []float32) error {
	line, err := json.Marshal(map[string]any{"id": id, "values": vector})
	if err != nil {
		return fmt.Errorf("vectorize upsert: encode request: %w", err)
	}
	var out envelope
	req := cfapi.Request{Method: http.MethodPost, Path: c.base + "/upsert", ContentType: "application/x-ndjson", Body: append(line, '\n')}
	if err := c.send(ctx, "upsert", req, &out); err != nil {
		return err
	}
	return out.err("upsert")
}

// DeleteByIDs removes vectors, splitting into requests of at most 1000 ids.
// Deleting an id that does not exist is not an error.
func (c *Client) DeleteByIDs(ctx context.Context, ids []string) error {
	for start := 0; start < len(ids); start += maxDeleteBatch {
		end := min(start+maxDeleteBatch, len(ids))
		body, err := json.Marshal(map[string]any{"ids": ids[start:end]})
		if err != nil {
			return fmt.Errorf("vectorize delete: encode request: %w", err)
		}
		var out envelope
		req := cfapi.Request{Method: http.MethodPost, Path: c.base + "/delete_by_ids", ContentType: "application/json", Body: bytes.Clone(body)}
		if err := c.send(ctx, "delete", req, &out); err != nil {
			return err
		}
		if err := out.err("delete"); err != nil {
			return err
		}
	}
	return nil
}
