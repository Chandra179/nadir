package ingest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DocumentConverter is the document-intake seam. It converts one supported
// source file into Markdown while preserving the caller's source identity.
type DocumentConverter interface {
	Convert(ctx context.Context, name string, data []byte) ([]byte, error)
}

type doclingConverter struct {
	addr   string
	client *http.Client
}

func NewDoclingConverter(addr string, timeout time.Duration) DocumentConverter {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &doclingConverter{
		addr:   strings.TrimRight(addr, "/"),
		client: &http.Client{Timeout: timeout},
	}
}

func (d *doclingConverter) Convert(ctx context.Context, name string, data []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.addr+"/convert", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("docling request: %w", err)
	}
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set("X-Nadir-Filename", name)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docling convert %s: %w", name, err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("docling read %s: %w", name, readErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("docling convert %s: status %s: %s", name, resp.Status, strings.TrimSpace(string(body)))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("docling convert %s: empty Markdown result", name)
	}
	return body, nil
}
