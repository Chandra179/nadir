//go:build js && wasm

package browser

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"syscall/js"
)

// Fetch serves one request through handler, streaming the response to the host.
//
//	request = { method, url, headers?, body?: Uint8Array|string, signal?: AbortSignal,
//	            onHead(status, headers), onChunk(Uint8Array) }
//
// onHead fires once before the first chunk; onChunk fires for every write, so
// Server-Sent Events reach the page as they are produced. The promise settles
// when the handler returns. Aborting signal cancels the request context.
func Fetch(ctx context.Context, handler http.Handler, request js.Value) error {
	method := request.Get("method").String()
	url := request.Get("url").String()
	if method == "" || url == "" {
		return fmt.Errorf("fetch: method and url are required")
	}
	var body []byte
	switch b := request.Get("body"); b.Type() {
	case js.TypeString:
		body = []byte(b.String())
	case js.TypeObject:
		body = make([]byte, b.Get("length").Int())
		js.CopyBytesToGo(body, b)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if signal := request.Get("signal"); signal.Type() == js.TypeObject {
		if signal.Get("aborted").Bool() {
			return context.Canceled
		}
		onAbort := js.FuncOf(func(js.Value, []js.Value) any { cancel(); return nil })
		signal.Call("addEventListener", "abort", onAbort)
		defer func() {
			signal.Call("removeEventListener", "abort", onAbort)
			onAbort.Release()
		}()
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://nadir.local"+url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if headers := request.Get("headers"); headers.Type() == js.TypeObject {
		keys := js.Global().Get("Object").Call("keys", headers)
		for i := 0; i < keys.Length(); i++ {
			key := keys.Index(i).String()
			req.Header.Set(key, headers.Get(key).String())
		}
	}

	w := &streamWriter{header: http.Header{}, onHead: request.Get("onHead"), onChunk: request.Get("onChunk")}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if !w.headSent() {
					w.WriteHeader(http.StatusInternalServerError)
				}
				err = fmt.Errorf("handler panic: %v", r)
			}
		}()
		handler.ServeHTTP(w, req)
	}()
	w.finish()
	return err
}

// streamWriter forwards each write to the host immediately.
type streamWriter struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	sent    bool
	onHead  js.Value
	onChunk js.Value
}

func (w *streamWriter) Header() http.Header { return w.header }

func (w *streamWriter) headSent() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sent
}

func (w *streamWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sendHeadLocked(status)
}

func (w *streamWriter) sendHeadLocked(status int) {
	if w.sent {
		return
	}
	w.sent, w.status = true, status
	headers := js.Global().Get("Object").New()
	for key, values := range w.header {
		if len(values) > 0 {
			headers.Set(key, values[0])
		}
	}
	if w.onHead.Type() == js.TypeFunction {
		w.onHead.Invoke(status, headers)
	}
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sendHeadLocked(http.StatusOK)
	if len(p) > 0 && w.onChunk.Type() == js.TypeFunction {
		chunk := js.Global().Get("Uint8Array").New(len(p))
		js.CopyBytesToJS(chunk, p)
		w.onChunk.Invoke(chunk)
	}
	return len(p), nil
}

// Flush satisfies http.Flusher; writes are already forwarded immediately.
func (w *streamWriter) Flush() {}

func (w *streamWriter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sendHeadLocked(http.StatusOK)
}
