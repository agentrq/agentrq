// Copyright 2026 Contextual, Inc. https://agentrq.com

package coremcp

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
)

// enumValues reads the `jsonschema:"enum: a, b, c"` tag off one field of a
// parameter struct, the way the MCP SDK does when it builds the tool schema an
// agent sees.
//
// Read by reflection rather than by parsing the source, so the test is checking
// the tag that actually ships.
func enumValues(t *testing.T, params any, field string) []string {
	t.Helper()

	f, ok := reflect.TypeOf(params).FieldByName(field)
	if !ok {
		t.Fatalf("%T has no field %s", params, field)
	}
	tag := f.Tag.Get("jsonschema")
	const prefix = "enum:"
	if !strings.HasPrefix(tag, prefix) {
		t.Fatalf("%T.%s has no enum in its jsonschema tag (got %q)", params, field, tag)
	}

	var values []string
	for _, v := range strings.Split(strings.TrimPrefix(tag, prefix), ",") {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		t.Fatalf("%T.%s declares an empty enum", params, field)
	}
	return values
}

// The enums an agent is shown must be the values the server accepts.
//
// A struct tag has to be a compile-time constant, so it cannot be built from
// the canonical slice in the controller — the list is written out twice by
// necessity. This is what stops the two copies drifting, and they had: the
// status enum advertised `waiting`, `done` and `failed`, none of which
// validate, and omitted `blocked` and `rejected`, both of which do. The action
// enum offered `deny`, which RespondToTask has never handled — one of only two
// values it advertised, so half of that tool's documented surface failed.
//
// Compared as sets: order is presentation, and pinning it would fail for a
// reordering that changes nothing.
func TestToolSchemaEnumsMatchTheValidators(t *testing.T) {
	tests := []struct {
		name   string
		params any
		field  string
		want   []string
	}{
		{
			name:   "updateTaskStatus offers the statuses the controller accepts",
			params: UpdateTaskStatusParams{},
			field:  "Status",
			want:   crud.ValidTaskStatuses,
		},
		{
			name:   "respondToTask offers the actions the controller handles",
			params: RespondToTaskParams{},
			field:  "Action",
			want:   crud.ValidTaskResponseActions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enumValues(t, tt.params, tt.field)

			for _, v := range got {
				if !slices.Contains(tt.want, v) {
					t.Errorf("schema offers %q, which the server rejects (accepted: %v)", v, tt.want)
				}
			}
			for _, v := range tt.want {
				if !slices.Contains(got, v) {
					t.Errorf("server accepts %q, but the schema does not offer it (offered: %v)", v, got)
				}
			}
		})
	}
}

// The assignee enums were already correct, and there are two of them — one on
// createTask and one on updateTaskAssignee — so they are pinned here to keep
// them agreeing with each other as well as with the controller.
func TestAssigneeEnumsAgree(t *testing.T) {
	want := []string{"agent", "human"}

	for _, tt := range []struct {
		name   string
		params any
	}{
		{"createTask", CreateTaskParams{}},
		{"updateTaskAssignee", UpdateTaskAssigneeParams{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := enumValues(t, tt.params, "Assignee")
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("expected assignees %v, got %v", want, got)
			}
		})
	}
}

// `blocked` is the status an agent uses to say it needs a human, so it earns an
// assertion of its own: a schema that omits it leaves an agent no way to ask,
// which is the whole point of the platform.
func TestStatusEnumOffersBlocked(t *testing.T) {
	if got := enumValues(t, UpdateTaskStatusParams{}, "Status"); !slices.Contains(got, "blocked") {
		t.Errorf("the status enum must offer 'blocked' so an agent can ask for help, got %v", got)
	}
}
