package docling

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	ingest "nadir/internal/knowledge/indexing"
	"nadir/internal/platform/observability"

	"go.uber.org/zap"
)

type Config struct {
	Addr           string
	RequestTimeout time.Duration
	Log            *zap.Logger
}

// Converter adapts the Docling HTTP service to Knowledge's Document intake
// seam. The original source identity is passed through unchanged for
// citations and deterministic chunk IDs.
type Converter struct {
	addr   string
	client *http.Client
	log    *zap.Logger
}

var _ ingest.DocumentConverter = (*Converter)(nil)

func New(cfg Config) *Converter {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Converter{
		addr:   strings.TrimRight(cfg.Addr, "/"),
		client: &http.Client{Timeout: timeout},
		log:    log,
	}
}

func (d *Converter) Convert(ctx context.Context, name string, data []byte) ([]byte, error) {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.addr+"/convert", bytes.NewReader(data))
	if err != nil {
		observability.Stage(d.log, "docling_conversion", "error", started, err, zap.String("name", name))
		return nil, fmt.Errorf("docling request: %w", err)
	}
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set("X-Nadir-Filename", name)
	resp, err := d.client.Do(req)
	if err != nil {
		observability.Stage(d.log, "docling_conversion", "error", started, err, zap.String("name", name))
		return nil, fmt.Errorf("docling convert %s: %w", name, err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		observability.Stage(d.log, "docling_conversion", "error", started, readErr, zap.String("name", name))
		return nil, fmt.Errorf("docling read %s: %w", name, readErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		err := fmt.Errorf("docling convert %s: status %s: %s", name, resp.Status, strings.TrimSpace(string(body)))
		observability.Stage(d.log, "docling_conversion", "error", started, err, zap.String("name", name))
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		err := fmt.Errorf("docling convert %s: empty Markdown result", name)
		observability.Stage(d.log, "docling_conversion", "error", started, err, zap.String("name", name))
		return nil, err
	}
	observability.Stage(d.log, "docling_conversion", "success", started, nil,
		zap.String("name", name), zap.Int("bytes", len(body)))
	return body, nil
}
