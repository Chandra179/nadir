package docling

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDoclingConverterPostsPDFAndReturnsMarkdown(t *testing.T) {
	converter := New(Config{Addr: "http://docling/", RequestTimeout: time.Second})
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

func TestDoclingConverterRejectsHTTPAndShapeErrors(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "status error", status: http.StatusBadGateway, body: "unavailable", wantErr: "status 502"},
		{name: "empty response", status: http.StatusOK, body: " \n", wantErr: "empty Markdown result"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			d := New(Config{Addr: srv.URL, RequestTimeout: time.Second})
			_, err := d.Convert(context.Background(), "report.pdf", []byte("pdf"))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Convert() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestDoclingConverterHonorsTimeoutAndCancellation(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()

	d := New(Config{Addr: srv.URL, RequestTimeout: 10 * time.Millisecond})
	if _, err := d.Convert(context.Background(), "report.pdf", []byte("pdf")); err == nil {
		t.Fatal("Convert() succeeded after client timeout")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := d.Convert(ctx, "report.pdf", []byte("pdf"))
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Docling request did not start")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("Convert() succeeded with canceled request")
	}
}
