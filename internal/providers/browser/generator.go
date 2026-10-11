//go:build js && wasm

package browser

import (
	"context"
	"fmt"
	"sync"
	"syscall/js"

	"nadir/internal/core/conversation/generation"
)

// Generator streams answer tokens from a host-page LLM (for example WebLLM).
type Generator struct {
	generate js.Value
}

// NewGenerator wraps generate(prompt, {signal, emit}) => Promise<void>. The
// host calls emit(token) for each token and resolves when the answer is
// complete; it must stop and reject or resolve when signal aborts.
func NewGenerator(generate js.Value) (*Generator, error) {
	if generate.Type() != js.TypeFunction {
		return nil, fmt.Errorf("generate must be a function")
	}
	return &Generator{generate: generate}, nil
}

// Generate returns a live event channel. Tokens queue without bound between
// the JS callback and the consumer so the callback never blocks the event loop.
func (g *Generator) Generate(ctx context.Context, prompt string) (<-chan generation.Event, error) {
	events := make(chan generation.Event, 16)
	controller := js.Global().Get("AbortController").New()

	var mu sync.Mutex
	var queue []string
	wake := make(chan struct{}, 1)
	emitFn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeString && args[0].String() != "" {
			mu.Lock()
			queue = append(queue, args[0].String())
			mu.Unlock()
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		return nil
	})
	control := js.Global().Get("Object").New()
	control.Set("signal", controller.Get("signal"))
	control.Set("emit", emitFn)

	settled := make(chan error, 1)
	go func() {
		// Wait for the host to settle even after cancellation: it may still
		// call emit until then, and a released js.Func must not be invoked.
		_, err := Await(context.Background(), g.generate.Invoke(prompt, control))
		emitFn.Release()
		settled <- err
	}()

	go func() {
		defer close(events)
		hasAnswer := false
		send := func(ev generation.Event) bool {
			select {
			case events <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		flush := func() bool {
			mu.Lock()
			pending := queue
			queue = nil
			mu.Unlock()
			for _, text := range pending {
				if !send(generation.Event{Kind: generation.EventToken, Text: text}) {
					return false
				}
				hasAnswer = true
			}
			return true
		}
		for {
			select {
			case <-wake:
				if !flush() {
					controller.Call("abort")
					return
				}
			case err := <-settled:
				if !flush() {
					return
				}
				switch {
				case ctx.Err() != nil:
				case err != nil:
					send(generation.Event{Kind: generation.EventError, Err: fmt.Errorf("generator: %w", err)})
				case !hasAnswer:
					send(generation.Event{Kind: generation.EventError, Err: fmt.Errorf("generator produced no answer content")})
				default:
					send(generation.Event{Kind: generation.EventDone})
				}
				return
			case <-ctx.Done():
				controller.Call("abort")
				return
			}
		}
	}()
	return events, nil
}
