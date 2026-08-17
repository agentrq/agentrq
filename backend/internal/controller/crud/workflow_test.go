package crud

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// step builds an event→event edge; workspace is irrelevant to cycle detection
// but set so the fixtures read like real rows.
func step(eventID, emitEventID int64) model.WorkflowStep {
	return model.WorkflowStep{
		EventID:     eventID,
		EmitEventID: emitEventID,
		WorkspaceID: 900,
	}
}

func TestWouldCreateCycle(t *testing.T) {
	tests := []struct {
		name        string
		existing    []model.WorkflowStep
		fromEventID int64
		emitEventID int64
		want        bool
	}{
		{
			name:        "leaf step never cycles",
			existing:    []model.WorkflowStep{step(1, 2)},
			fromEventID: 2,
			emitEventID: 0,
			want:        false,
		},
		{
			name:        "self loop",
			existing:    nil,
			fromEventID: 1,
			emitEventID: 1,
			want:        true,
		},
		{
			name:        "first edge in an empty workflow",
			existing:    nil,
			fromEventID: 1,
			emitEventID: 2,
			want:        false,
		},
		{
			name:        "linear chain stays acyclic",
			existing:    []model.WorkflowStep{step(1, 2), step(2, 3)},
			fromEventID: 3,
			emitEventID: 4,
			want:        false,
		},
		{
			name:        "two-hop back edge closes the loop",
			existing:    []model.WorkflowStep{step(1, 2)},
			fromEventID: 2,
			emitEventID: 1,
			want:        true,
		},
		{
			name:        "long chain back edge closes the loop",
			existing:    []model.WorkflowStep{step(1, 2), step(2, 3), step(3, 4)},
			fromEventID: 4,
			emitEventID: 1,
			want:        true,
		},
		{
			name:        "back edge into the middle of a chain",
			existing:    []model.WorkflowStep{step(1, 2), step(2, 3), step(3, 4)},
			fromEventID: 4,
			emitEventID: 2,
			want:        true,
		},
		{
			name:        "fan-out from one event is not a cycle",
			existing:    []model.WorkflowStep{step(1, 2)},
			fromEventID: 1,
			emitEventID: 3,
			want:        false,
		},
		{
			name:        "fan-in to one event is not a cycle",
			existing:    []model.WorkflowStep{step(1, 3)},
			fromEventID: 2,
			emitEventID: 3,
			want:        false,
		},
		{
			name: "diamond re-join is not a cycle",
			existing: []model.WorkflowStep{
				step(1, 2),
				step(1, 3),
				step(2, 4),
			},
			fromEventID: 3,
			emitEventID: 4,
			want:        false,
		},
		{
			name: "disconnected component does not create a false positive",
			existing: []model.WorkflowStep{
				step(1, 2),
				step(10, 11),
			},
			fromEventID: 11,
			emitEventID: 12,
			want:        false,
		},
		{
			name: "edge into a separate existing cycle does not hang",
			// A pre-existing cycle should be unreachable through the API, but
			// the walk must still terminate if one is ever present in storage.
			existing: []model.WorkflowStep{
				step(10, 11),
				step(11, 10),
			},
			fromEventID: 1,
			emitEventID: 10,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wouldCreateCycle(tt.existing, tt.fromEventID, tt.emitEventID)
			if got != tt.want {
				t.Errorf("wouldCreateCycle(%d -> %d) = %v, want %v", tt.fromEventID, tt.emitEventID, got, tt.want)
			}
		})
	}
}
