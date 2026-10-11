//go:build js && wasm

package browser

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"syscall/js"
)

// Embedder delegates embedding to host-page functions, typically a
// transformers.js pipeline running in a worker. The host decides the model;
// task prefixes are already applied by the callers.
type Embedder struct {
	embed      js.Value
	embedBatch js.Value
	dimensions int
}

// NewEmbedder wraps embed(text) and the optional embedBatch(texts), each
// returning a promise of a Float32Array or number array (a batch is an array of
// those). Pass an undefined embedBatch to embed sequentially.
func NewEmbedder(embed, embedBatch js.Value, dimensions int) (*Embedder, error) {
	if embed.Type() != js.TypeFunction {
		return nil, fmt.Errorf("embed must be a function")
	}
	if dimensions <= 0 {
		return nil, fmt.Errorf("dimensions must be positive, got %d", dimensions)
	}
	return &Embedder{embed: embed, embedBatch: embedBatch, dimensions: dimensions}, nil
}

func (e *Embedder) Dimensions() int { return e.dimensions }

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := Await(ctx, e.embed.Invoke(text))
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	return e.vector(result)
}

// EmbedBatch embeds texts in one host call when the host supplied embedBatch.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if e.embedBatch.Type() != js.TypeFunction {
		out := make([][]float32, len(texts))
		for i, text := range texts {
			vector, err := e.Embed(ctx, text)
			if err != nil {
				return nil, err
			}
			out[i] = vector
		}
		return out, nil
	}
	input := js.Global().Get("Array").New(len(texts))
	for i, text := range texts {
		input.SetIndex(i, text)
	}
	result, err := Await(ctx, e.embedBatch.Invoke(input))
	if err != nil {
		return nil, fmt.Errorf("embed batch: %w", err)
	}
	if got := result.Length(); got != len(texts) {
		return nil, fmt.Errorf("embed batch: got %d vectors for %d texts", got, len(texts))
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		if out[i], err = e.vector(result.Index(i)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (e *Embedder) vector(v js.Value) ([]float32, error) {
	vector, err := Float32s(v)
	if err != nil {
		return nil, err
	}
	if len(vector) != e.dimensions {
		return nil, fmt.Errorf("embedding has %d dimensions, want %d", len(vector), e.dimensions)
	}
	return vector, nil
}

// Float32s copies a Float32Array or a plain number array into Go.
func Float32s(v js.Value) ([]float32, error) {
	if v.Type() != js.TypeObject {
		return nil, fmt.Errorf("embedding must be a Float32Array or array of numbers")
	}
	n := v.Length()
	out := make([]float32, n)
	if v.InstanceOf(js.Global().Get("Float32Array")) {
		raw := make([]byte, n*4)
		view := js.Global().Get("Uint8Array").New(v.Get("buffer"), v.Get("byteOffset"), n*4)
		js.CopyBytesToGo(raw, view)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return out, nil
	}
	for i := range out {
		out[i] = float32(v.Index(i).Float())
	}
	return out, nil
}
