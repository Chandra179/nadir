// Package kv is a small client for Cloudflare Workers KV's REST API
// (/accounts/{id}/storage/kv/namespaces/{namespace}). KV is eventually
// consistent across locations (up to about a minute) and enforces a minimum
// expiration of 60 seconds. This client owns request and response envelopes
// only; the request shapes were not verified against live KV.
package kv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nadir/internal/providers/cloudflare/cfapi"
)

// MinTTL is KV's minimum expiration.
const MinTTL = 60 * time.Second

// maxDeleteBatch is the most keys sent to one bulk delete request.
const maxDeleteBatch = 10000

// Config groups the account, namespace and credential. APIToken is a secret:
// it is sent only as a bearer header and never logged or included in errors.
type Config struct {
	AccountID      string
	APIToken       string
	NamespaceID    string
	BaseURL        string // empty uses the Cloudflare default; tests point it at httptest
	RequestTimeout time.Duration
}

// Client runs operations against one KV namespace.
type Client struct {
	api  *cfapi.Client
	base string
}

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.NamespaceID) == "" {
		return nil, fmt.Errorf("kv namespace id is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	api, err := cfapi.New(cfapi.Config{AccountID: cfg.AccountID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL, RequestTimeout: timeout})
	if err != nil {
		return nil, err
	}
	return &Client{api: api, base: "/storage/kv/namespaces/" + url.PathEscape(cfg.NamespaceID)}, nil
}

// Put stores value under key. A positive ttl sets an expiration, raised to
// MinTTL when shorter; zero means the key never expires.
func (c *Client) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	query := url.Values{}
	if ttl > 0 {
		query.Set("expiration_ttl", strconv.Itoa(int(max(ttl, MinTTL)/time.Second)))
	}
	resp, err := c.api.Do(ctx, cfapi.Request{
		Method: http.MethodPut, Path: c.base + "/values/" + url.PathEscape(key), Query: query,
		ContentType: "application/octet-stream", Body: value,
	})
	if err != nil {
		return fmt.Errorf("kv put: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// Get returns the value for key, and false when the key does not exist.
func (c *Client) Get(ctx context.Context, key string) ([]byte, bool, error) {
	resp, err := c.api.Do(ctx, cfapi.Request{Method: http.MethodGet, Path: c.base + "/values/" + url.PathEscape(key)})
	if err != nil {
		var status *cfapi.StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("kv get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	value, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, false, fmt.Errorf("kv get: read value: %w", err)
	}
	return value, true, nil
}

// ListKeys returns up to 1000 key names with the prefix, and the cursor for the
// next page ("" when there are no more).
func (c *Client) ListKeys(ctx context.Context, prefix, cursor string) ([]string, string, error) {
	query := url.Values{"limit": {"1000"}}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	resp, err := c.api.Do(ctx, cfapi.Request{Method: http.MethodGet, Path: c.base + "/keys", Query: query})
	if err != nil {
		return nil, "", fmt.Errorf("kv list: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Success bool `json:"success"`
		Result  []struct {
			Name string `json:"name"`
		} `json:"result"`
		ResultInfo struct {
			Cursor string `json:"cursor"`
		} `json:"result_info"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, "", fmt.Errorf("kv list: decode response: %w", err)
	}
	if !out.Success {
		return nil, "", fmt.Errorf("kv list: unsuccessful response")
	}
	names := make([]string, len(out.Result))
	for i, k := range out.Result {
		names[i] = k.Name
	}
	return names, out.ResultInfo.Cursor, nil
}

// DeleteMany removes keys in bulk, splitting into requests of at most 10000.
// Deleting a key that does not exist is not an error.
func (c *Client) DeleteMany(ctx context.Context, keys []string) error {
	for start := 0; start < len(keys); start += maxDeleteBatch {
		end := min(start+maxDeleteBatch, len(keys))
		body, err := json.Marshal(keys[start:end])
		if err != nil {
			return fmt.Errorf("kv delete: encode request: %w", err)
		}
		resp, err := c.api.Do(ctx, cfapi.Request{Method: http.MethodPost, Path: c.base + "/bulk/delete", ContentType: "application/json", Body: body})
		if err != nil {
			return fmt.Errorf("kv delete: %w", err)
		}
		_ = resp.Body.Close()
	}
	return nil
}
