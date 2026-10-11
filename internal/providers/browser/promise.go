//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall/js"
)

// Await blocks the calling goroutine until the JavaScript promise (or plain
// value) settles or ctx ends. It must not run on the JS event-loop callback
// itself; exported functions go through NewPromise, which runs work in a
// goroutine.
func Await(ctx context.Context, value js.Value) (js.Value, error) {
	type outcome struct {
		value js.Value
		err   error
	}
	settled := make(chan outcome, 1)
	promise := js.Global().Get("Promise").Call("resolve", value)

	var once sync.Once
	var onOK, onFail js.Func
	release := func() {
		once.Do(func() {
			onOK.Release()
			onFail.Release()
		})
	}
	onOK = js.FuncOf(func(_ js.Value, args []js.Value) any {
		settled <- outcome{value: args[0]}
		release()
		return nil
	})
	onFail = js.FuncOf(func(_ js.Value, args []js.Value) any {
		settled <- outcome{err: Error(args[0])}
		release()
		return nil
	})
	promise.Call("then", onOK, onFail)

	// On cancellation the callbacks stay registered until the promise settles:
	// releasing a js.Func the runtime may still call would panic.
	select {
	case result := <-settled:
		return result.value, result.err
	case <-ctx.Done():
		return js.Undefined(), ctx.Err()
	}
}

// Error converts a rejected JavaScript value to a Go error.
func Error(v js.Value) error {
	if v.Type() == js.TypeObject {
		if message := v.Get("message"); message.Type() == js.TypeString {
			return errors.New(message.String())
		}
	}
	return fmt.Errorf("js: %s", js.Global().Get("String").Invoke(v).String())
}

// NewPromise runs fn in a goroutine and returns a JavaScript promise for its
// result. A panic rejects the promise instead of killing the runtime.
func NewPromise(fn func() (any, error)) js.Value {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve, reject := args[0], args[1]
		go func() {
			defer func() {
				if r := recover(); r != nil {
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprint("panic: ", r)))
				}
			}()
			result, err := fn()
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(err.Error()))
				return
			}
			resolve.Invoke(result)
		}()
		return nil
	})
	// The executor runs synchronously inside New, so it is safe to release.
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()
	return promise
}
