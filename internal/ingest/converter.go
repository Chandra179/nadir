package ingest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"nadir/internal/observability"

	"go.uber.org/zap"
)

type doclingConverter struct {
	addr   string
	client *http.Client
	log    *zap.Logger
}

func (d *doclingConverter) Convert(ctx context.Context, name string, data []byte) ([]byte, error) {
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
