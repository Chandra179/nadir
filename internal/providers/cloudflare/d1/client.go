// Package d1 is a small client for Cloudflare D1's REST query endpoint
// (POST /accounts/{id}/d1/database/{db}/query). It owns the request and result
// envelope; SQL and row meaning belong to the adapters that use it.
//
// A batch of statements is sent as one request. D1 documents batches as
// transactional, but the REST form was not verified live here, so adapters
// keep their writes correct even if a batch were applied partially.
package d1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nadir/internal/providers/cloudflare/cfapi"
)

// Config groups the account, database and credential. APIToken is a secret: it
// is sent only as a bearer header and never logged or included in errors.
type Config struct {
	AccountID      string
	APIToken       string
	DatabaseID     string
	BaseURL        string // empty uses the Cloudflare default; tests point it at httptest
	RequestTimeout time.Duration
}

// Client runs SQL against one D1 database.
type Client struct {
	api  *cfapi.Client
	path string
}

// Statement is one SQL statement with positional (?) parameters.
type Statement struct {
	SQL    string
	Params []any
}

// Row is one result row keyed by column name. Numbers decode as json.Number so
// 64-bit integers such as millisecond timestamps stay exact.
type Row map[string]any

// New validates cfg and constructs a Client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.DatabaseID) == "" {
		return nil, fmt.Errorf("d1 database id is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	api, err := cfapi.New(cfapi.Config{
		AccountID: cfg.AccountID, APIToken: cfg.APIToken, BaseURL: cfg.BaseURL, RequestTimeout: timeout,
		// A 429 was rejected before execution; a 503 may not have been, and
		// replaying a write could duplicate it.
		RetryStatuses: []int{http.StatusTooManyRequests},
	})
	if err != nil {
		return nil, err
	}
	return &Client{api: api, path: "/d1/database/" + cfg.DatabaseID + "/query"}, nil
}

type wireStatement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params,omitempty"`
}

type wireResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result []struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
		Results []Row  `json:"results"`
	} `json:"result"`
}

// Exec runs statements in order and returns one row set per statement. One
// statement is sent as a plain query, several as a batch.
func (c *Client) Exec(ctx context.Context, statements ...Statement) ([][]Row, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	var body any
	if len(statements) == 1 {
		body = wireStatement{SQL: statements[0].SQL, Params: statements[0].Params}
	} else {
		batch := make([]wireStatement, len(statements))
		for i, s := range statements {
			batch[i] = wireStatement{SQL: s.SQL, Params: s.Params}
		}
		body = struct {
			Batch []wireStatement `json:"batch"`
		}{batch}
	}
	resp, err := c.api.Post(ctx, c.path, body, nil)
	if err != nil {
		return nil, fmt.Errorf("d1: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var out wireResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("d1: decode response: %w", err)
	}
	if !out.Success {
		msg := "unsuccessful response"
		if len(out.Errors) > 0 && out.Errors[0].Message != "" {
			msg = out.Errors[0].Message
		}
		return nil, fmt.Errorf("d1: %s", msg)
	}
	if len(out.Result) != len(statements) {
		return nil, fmt.Errorf("d1: got %d results for %d statements", len(out.Result), len(statements))
	}
	rows := make([][]Row, len(out.Result))
	for i, r := range out.Result {
		if r.Success != nil && !*r.Success {
			msg := r.Error
			if msg == "" {
				msg = "statement failed"
			}
			return nil, fmt.Errorf("d1: statement %d: %s", i+1, msg)
		}
		rows[i] = r.Results
	}
	return rows, nil
}

// Str returns a text column, or "" when it is NULL or absent.
func (r Row) Str(key string) string {
	s, _ := r[key].(string)
	return s
}

// Int returns an integer column, or 0 when it is NULL, absent or not integral.
func (r Row) Int(key string) int64 {
	switch v := r[key].(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			f, ferr := v.Float64()
			if ferr != nil {
				return 0
			}
			return int64(f)
		}
		return n
	case float64:
		return int64(v)
	}
	return 0
}

// Bool returns an integer column read as a boolean (non-zero is true).
func (r Row) Bool(key string) bool { return r.Int(key) != 0 }
