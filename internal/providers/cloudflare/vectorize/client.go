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
	api       *cfapi.Client
	base      string
	pollEvery time.Duration
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
	return &Client{api: api, base: "/vectorize/v2/indexes/" + url.PathEscape(cfg.Index), pollEvery: 500 * time.Millisecond}, nil
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

// QueryOptions narrows a query. Filter keys are indexed metadata properties and
// each must equal its value; the index must have a metadata index for the key
// before vectors carrying it are inserted.
type QueryOptions struct {
	TopK   int
	Filter map[string]string
}

// Query returns up to topK nearest vectors, best first, without values or
// metadata.
func (c *Client) Query(ctx context.Context, vector []float32, topK int) ([]Match, error) {
	return c.QueryWith(ctx, vector, QueryOptions{TopK: topK})
}

// QueryWith is Query with a metadata equality filter. A query returns ids only,
// which Vectorize allows up to a higher topK than a query returning metadata.
func (c *Client) QueryWith(ctx context.Context, vector []float32, opts QueryOptions) ([]Match, error) {
	request := map[string]any{"vector": vector, "topK": opts.TopK, "returnValues": false, "returnMetadata": "none"}
	if len(opts.Filter) > 0 {
		request["filter"] = opts.Filter
	}
	body, err := json.Marshal(request)
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

// Vector is one vector to store. Metadata values must be strings.
type Vector struct {
	ID       string
	Values   []float32
	Metadata map[string]string
}

// Upsert inserts or replaces the vector with this id. The write is accepted
// immediately and applied asynchronously.
func (c *Client) Upsert(ctx context.Context, id string, vector []float32) error {
	_, err := c.UpsertMany(ctx, []Vector{{ID: id, Values: vector}})
	return err
}

// UpsertMany inserts or replaces vectors in one NDJSON request and returns the
// mutation id, which WaitForMutation can wait on. The write is accepted
// immediately and applied asynchronously.
func (c *Client) UpsertMany(ctx context.Context, vectors []Vector) (string, error) {
	if len(vectors) == 0 {
		return "", nil
	}
	var body bytes.Buffer
	for _, v := range vectors {
		line := map[string]any{"id": v.ID, "values": v.Values}
		if len(v.Metadata) > 0 {
			line["metadata"] = v.Metadata
		}
		raw, err := json.Marshal(line)
		if err != nil {
			return "", fmt.Errorf("vectorize upsert: encode request: %w", err)
		}
		body.Write(raw)
		body.WriteByte('\n')
	}
	var out struct {
		envelope
		Result struct {
			MutationID string `json:"mutationId"`
		} `json:"result"`
	}
	req := cfapi.Request{Method: http.MethodPost, Path: c.base + "/upsert", ContentType: "application/x-ndjson", Body: body.Bytes()}
	if err := c.send(ctx, "upsert", req, &out); err != nil {
		return "", err
	}
	if err := out.err("upsert"); err != nil {
		return "", err
	}
	return out.Result.MutationID, nil
}

// MetadataIndex is an indexed metadata property.
type MetadataIndex struct {
	Property string
	Type     string
}

// MetadataIndexes lists the index's metadata indexes.
func (c *Client) MetadataIndexes(ctx context.Context) ([]MetadataIndex, error) {
	var out struct {
		envelope
		Result struct {
			Indexes []struct {
				PropertyName string `json:"propertyName"`
				IndexType    string `json:"indexType"`
			} `json:"metadataIndexes"`
		} `json:"result"`
	}
	if err := c.send(ctx, "metadata indexes", cfapi.Request{Method: http.MethodGet, Path: c.base + "/metadata_index/list"}, &out); err != nil {
		return nil, err
	}
	if err := out.err("metadata indexes"); err != nil {
		return nil, err
	}
	indexes := make([]MetadataIndex, len(out.Result.Indexes))
	for i, m := range out.Result.Indexes {
		indexes[i] = MetadataIndex{Property: m.PropertyName, Type: m.IndexType}
	}
	return indexes, nil
}

// WaitForMutation polls the index until the mutation has been applied, so a
// vector written with it can be found by queries. It returns false when the
// timeout passes first; callers decide whether that is fatal. An empty
// mutation id returns true at once.
func (c *Client) WaitForMutation(ctx context.Context, mutationID string, timeout time.Duration) (bool, error) {
	if mutationID == "" {
		return true, nil
	}
	deadline := time.Now().Add(timeout)
	for {
		var out struct {
			envelope
			Result struct {
				ProcessedUpToMutation string `json:"processedUpToMutation"`
			} `json:"result"`
		}
		if err := c.send(ctx, "info", cfapi.Request{Method: http.MethodGet, Path: c.base + "/info"}, &out); err != nil {
			return false, err
		}
		if err := out.err("info"); err != nil {
			return false, err
		}
		if out.Result.ProcessedUpToMutation == mutationID {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		timer := time.NewTimer(c.pollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
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
