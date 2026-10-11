//go:build js && wasm

package browser

import (
	"context"
	"fmt"
	"syscall/js"
)

// Converter turns uploaded binary documents such as PDFs into Markdown or text
// with a host-page extractor (for example pdf.js).
type Converter struct {
	convert js.Value
}

// NewConverter wraps convert(name, bytes) => Promise<Uint8Array|string>.
func NewConverter(convert js.Value) (*Converter, error) {
	if convert.Type() != js.TypeFunction {
		return nil, fmt.Errorf("convert must be a function")
	}
	return &Converter{convert: convert}, nil
}

func (c *Converter) Convert(ctx context.Context, name string, data []byte) ([]byte, error) {
	input := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(input, data)
	result, err := Await(ctx, c.convert.Invoke(name, input))
	if err != nil {
		return nil, fmt.Errorf("convert %q: %w", name, err)
	}
	if result.Type() == js.TypeString {
		return []byte(result.String()), nil
	}
	if result.Type() != js.TypeObject {
		return nil, fmt.Errorf("convert %q: result must be a string or Uint8Array", name)
	}
	out := make([]byte, result.Get("length").Int())
	js.CopyBytesToGo(out, result)
	return out, nil
}
