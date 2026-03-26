package eventbus

import (
	"bytes"
	"testing"
)

func TestEventBus(t *testing.T) {
	bus := New()
	workspaceID := int64(123)

	t.Run("SubscribeAndPublish", func(t *testing.T) {
		ch := bus.Subscribe(workspaceID)
		evt := Event{Type: "test", Payload: "data"}
		
		go bus.Publish(workspaceID, evt)
		
		msg := <-ch
		if !bytes.Contains(msg, []byte(`"type":"test"`)) {
			t.Errorf("unexpected message: %s", string(msg))
		}
		
		bus.Unsubscribe(workspaceID, ch)
	})

	t.Run("UnsubscribeNonExistent", func(t *testing.T) {
		ch := make(chan []byte)
		bus.Unsubscribe(999, ch) // should not panic
	})

	t.Run("SlowConsumer", func(t *testing.T) {
		ch := bus.Subscribe(workspaceID)
		// fill the buffer (32)
		for i := 0; i < 35; i++ {
			bus.Publish(workspaceID, Event{Type: "drop"})
		}
		// Should not block
		bus.Unsubscribe(workspaceID, ch)
	})

	t.Run("MarshalError", func(t *testing.T) {
		// Circular reference or unsupported type to force marshal error
		evt := Event{Type: "error", Payload: make(chan int)}
		bus.Publish(workspaceID, evt) // should just return early
	})
}
