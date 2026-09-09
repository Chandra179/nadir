package ingest

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDoclingConverterPostsPDFAndReturnsMarkdown(t *testing.T) {
	converter := NewDoclingConverter("http://docling/", time.Second).(*doclingConverter)
	converter.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/convert" {
			t.Fatalf("request = %s %s, want POST /convert", req.Method, req.URL.Path)
		}
		if req.Header.Get("Content-Type") != "application/pdf" || req.Header.Get("X-Nadir-Filename") != "report.pdf" {
			t.Fatalf("headers = %#v, missing PDF intake headers", req.Header)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "pdf" {
			t.Fatalf("body = %q, want pdf", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("# Markdown")),
		}, nil
	})

	got, err := converter.Convert(t.Context(), "report.pdf", []byte("pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Markdown" {
		t.Fatalf("converted = %q, want Markdown response", got)
	}
}
