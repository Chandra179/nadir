package generation

// EventKind identifies an item in a generation stream.
type EventKind uint8

const (
	EventToken EventKind = iota
	EventError
	EventDone
)

// Event is one item of a generation stream. A concrete value keeps the stream
// contract easy to serialize, fake, and evolve without a public type family.
type Event struct {
	Kind EventKind
	Text string
	Err  error
}
