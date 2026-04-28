package idgen

import (
	"testing"
)

func TestIdgen(t *testing.T) {
	s, err := New(1)
	if err != nil {
		t.Fatalf("failed to create idgen: %v", err)
	}

	t.Run("NextID", func(t *testing.T) {
		id1 := s.NextID()
		id2 := s.NextID()
		if id1 == 0 {
			t.Error("expected non-zero ID")
		}
		if id1 == id2 {
			t.Error("expected different IDs")
		}
	})
}
