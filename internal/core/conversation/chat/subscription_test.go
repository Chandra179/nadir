package chat

import (
	"context"
	"testing"
	"testing/synctest"
)

func TestSubscribeDetachDoesNotLeaveWatcher(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dependencies{broker: newBroker()}
		stream, _ := d.broker.create("turn")
		generationCanceled := false
		stream.setCancel(func() { generationCanceled = true })
		events, detach, ok := d.Subscribe(context.Background(), "turn", 0)
		if !ok {
			t.Fatal("turn should be available")
		}
		detach()
		detach()
		if _, open := <-events; open {
			t.Fatal("detached subscriber should be closed")
		}
		if generationCanceled {
			t.Fatal("detaching a subscriber must not cancel generation")
		}
		// synctest fails if a background-context watcher remains blocked after
		// this callback returns. No goroutine count or timing assumption is used.
	})
}

func TestSubscribeCancellationRacesDetach(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dependencies{broker: newBroker()}
		stream, _ := d.broker.create("turn")
		generationCanceled := false
		stream.setCancel(func() { generationCanceled = true })
		ctx, cancel := context.WithCancel(context.Background())
		events, detach, _ := d.Subscribe(ctx, "turn", 0)
		go detach()
		cancel()
		synctest.Wait()
		if _, open := <-events; open {
			t.Fatal("canceled subscriber should be closed")
		}
		if generationCanceled {
			t.Fatal("subscriber cancellation must not cancel generation")
		}
	})
}

func TestSubscribeContextCancellationDetaches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &dependencies{broker: newBroker()}
		d.broker.create("turn")
		ctx, cancel := context.WithCancel(context.Background())
		events, detach, _ := d.Subscribe(ctx, "turn", 0)
		cancel()
		synctest.Wait()
		if _, open := <-events; open {
			t.Fatal("context cancellation should close the subscriber")
		}
		detach()
	})
}
