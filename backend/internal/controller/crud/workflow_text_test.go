package crud

import (
	"strings"
	"testing"
	"time"
)

// The example from the feature request, in its corrected form.
const canonicalDoc = `workflow: new_feature
event: code_changed
- agent:doc
  - event:doc_updated
- agent:blog
  - event:new_feature_added
    - agent:twitter
    - agent:hackernews
    - agent:linkedin
`

func TestParseWorkflowTextCanonical(t *testing.T) {
	parsed, err := ParseWorkflowText(canonicalDoc)
	if err != nil {
		t.Fatalf("ParseWorkflowText: %v", err)
	}

	if parsed.Name != "new_feature" {
		t.Errorf("Name = %q, want %q", parsed.Name, "new_feature")
	}
	if parsed.StartEvent != "code_changed" {
		t.Errorf("StartEvent = %q, want %q", parsed.StartEvent, "code_changed")
	}
	if len(parsed.Roots) != 2 {
		t.Fatalf("len(Roots) = %d, want 2", len(parsed.Roots))
	}

	doc := parsed.Roots[0]
	if doc.Kind != "agent" || doc.Name != "doc" {
		t.Errorf("Roots[0] = %s:%s, want agent:doc", doc.Kind, doc.Name)
	}
	if len(doc.Children) != 1 || doc.Children[0].Name != "doc_updated" {
		t.Fatalf("doc should emit doc_updated, got %+v", doc.Children)
	}
	if len(doc.Children[0].Children) != 0 {
		t.Errorf("doc_updated should have no consumers, got %d", len(doc.Children[0].Children))
	}

	blog := parsed.Roots[1]
	if blog.Name != "blog" {
		t.Fatalf("Roots[1] = %q, want blog", blog.Name)
	}
	emitted := blog.Children[0]
	if emitted.Kind != "event" || emitted.Name != "new_feature_added" {
		t.Fatalf("blog should emit event:new_feature_added, got %s:%s", emitted.Kind, emitted.Name)
	}
	if len(emitted.Children) != 3 {
		t.Fatalf("new_feature_added should have 3 consumers, got %d", len(emitted.Children))
	}
	for i, want := range []string{"twitter", "hackernews", "linkedin"} {
		if got := emitted.Children[i].Name; got != want {
			t.Errorf("consumer[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestParseWorkflowTextFlatten(t *testing.T) {
	parsed, err := ParseWorkflowText(canonicalDoc)
	if err != nil {
		t.Fatalf("ParseWorkflowText: %v", err)
	}
	steps := FlattenWorkflowText(parsed)

	want := []TextStep{
		{EventName: "code_changed", WorkspaceName: "doc", EmitEventName: "doc_updated"},
		{EventName: "code_changed", WorkspaceName: "blog", EmitEventName: "new_feature_added"},
		{EventName: "new_feature_added", WorkspaceName: "twitter"},
		{EventName: "new_feature_added", WorkspaceName: "hackernews"},
		{EventName: "new_feature_added", WorkspaceName: "linkedin"},
	}
	if len(steps) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(steps), len(want), steps)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Errorf("step[%d] = %+v, want %+v", i, steps[i], want[i])
		}
	}
}

func TestParseWorkflowTextRequiresKindPrefix(t *testing.T) {
	// Every list item carries its kind, so a line's meaning never depends on
	// its depth. A bare name is a typo, and reporting it beats guessing —
	// guessing is how a workflow silently routes somewhere the author did not
	// intend.
	bare := `workflow: w
event: start
- agent:doc
  - doc_updated
`
	err := mustParseError(t, bare)
	if !strings.Contains(err.Error(), "event:<name>") {
		t.Errorf("error should point at the prefixed form, got %q", err)
	}
	if err.Line != 4 {
		t.Errorf("Line = %d, want 4", err.Line)
	}
}

// mustParseError asserts the input fails to parse and returns the typed error.
func mustParseError(t *testing.T, input string) *WorkflowTextError {
	t.Helper()
	_, err := ParseWorkflowText(input)
	if err == nil {
		t.Fatal("expected a parse error, got nil")
	}
	te, ok := err.(*WorkflowTextError)
	if !ok {
		t.Fatalf("expected *WorkflowTextError, got %T", err)
	}
	return te
}

func TestParseWorkflowTextErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLine int
		wantMsg  string
	}{
		{
			name:    "missing workflow line",
			input:   "event: start\n- agent:a\n",
			wantMsg: "missing \"workflow:",
		},
		{
			name:    "missing start event",
			input:   "workflow: w\n",
			wantMsg: "missing \"event:",
		},
		{
			name:     "tabs rejected",
			input:    "workflow: w\nevent: s\n- agent:a\n\t- event:e\n",
			wantLine: 4,
			wantMsg:  "tabs",
		},
		{
			name:     "odd indentation",
			input:    "workflow: w\nevent: s\n- agent:a\n   - event:e\n",
			wantLine: 4,
			wantMsg:  "indent by 2 spaces",
		},
		{
			name:     "indentation jump",
			input:    "workflow: w\nevent: s\n- agent:a\n    - event:e\n",
			wantLine: 4,
			wantMsg:  "jumped",
		},
		{
			name:     "agent under agent breaks alternation",
			input:    "workflow: w\nevent: s\n- agent:a\n  - agent:b\n",
			wantLine: 4,
			wantMsg:  "may only emit an event",
		},
		{
			name:     "event under event breaks alternation",
			input:    "workflow: w\nevent: s\n- agent:a\n  - event:e\n    - event:e2\n",
			wantLine: 5,
			wantMsg:  "expected \"- agent:",
		},
		{
			name:     "event at root breaks alternation",
			input:    "workflow: w\nevent: s\n- event:e\n",
			wantLine: 3,
			wantMsg:  "expected \"- agent:",
		},
		{
			name:     "agent emitting two events",
			input:    "workflow: w\nevent: s\n- agent:a\n  - event:e1\n  - event:e2\n",
			wantLine: 5,
			wantMsg:  "at most one event",
		},
		{
			name:     "unknown kind",
			input:    "workflow: w\nevent: s\n- robot:a\n",
			wantLine: 3,
			wantMsg:  "unknown kind",
		},
		{
			name:     "missing colon in list item",
			input:    "workflow: w\nevent: s\n- doc\n",
			wantLine: 3,
			wantMsg:  "expected \"agent:<name>\"",
		},
		{
			name:     "empty agent name",
			input:    "workflow: w\nevent: s\n- agent:\n",
			wantLine: 3,
			wantMsg:  "name cannot be empty",
		},
		{
			name:     "duplicate start event",
			input:    "workflow: w\nevent: s\nevent: s2\n",
			wantLine: 3,
			wantMsg:  "already set",
		},
		{
			name:     "duplicate workflow name",
			input:    "workflow: w\nworkflow: w2\n",
			wantLine: 2,
			wantMsg:  "already set",
		},
		{
			name:     "unknown header field",
			input:    "workflow: w\nnope: x\n",
			wantLine: 2,
			wantMsg:  "unknown field",
		},
		{
			name:     "list item before start event",
			input:    "workflow: w\n- agent:a\n",
			wantLine: 2,
			wantMsg:  "must come after",
		},
		{
			name:     "indented header",
			input:    "workflow: w\n  event: s\n",
			wantLine: 2,
			wantMsg:  "must not be indented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseWorkflowText(tt.input)
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.wantMsg)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
			if tt.wantLine > 0 {
				te, ok := err.(*WorkflowTextError)
				if !ok {
					t.Fatalf("error should be *WorkflowTextError so the editor can mark the line, got %T", err)
				}
				if te.Line != tt.wantLine {
					t.Errorf("Line = %d, want %d", te.Line, tt.wantLine)
				}
			}
		})
	}
}

func TestParseWorkflowTextIgnoresBlanksAndComments(t *testing.T) {
	input := `# the release pipeline

workflow: w

event: start

# docs first
- agent:doc

  - event:doc_updated
`
	parsed, err := ParseWorkflowText(input)
	if err != nil {
		t.Fatalf("ParseWorkflowText: %v", err)
	}
	if len(parsed.Roots) != 1 || parsed.Roots[0].Name != "doc" {
		t.Fatalf("expected one agent:doc root, got %+v", parsed.Roots)
	}
	if len(parsed.Roots[0].Children) != 1 {
		t.Fatalf("doc should still emit its event across the blank line")
	}
}

func TestParseWorkflowTextDedentBackToRoot(t *testing.T) {
	// After descending three levels, a root-level item must reattach to the
	// start event rather than to whatever was last open.
	input := `workflow: w
event: start
- agent:a
  - event:mid
    - agent:b
- agent:c
`
	parsed, err := ParseWorkflowText(input)
	if err != nil {
		t.Fatalf("ParseWorkflowText: %v", err)
	}
	if len(parsed.Roots) != 2 {
		t.Fatalf("len(Roots) = %d, want 2", len(parsed.Roots))
	}
	if parsed.Roots[1].Name != "c" {
		t.Errorf("Roots[1] = %q, want c", parsed.Roots[1].Name)
	}
}

func TestRenderWorkflowText(t *testing.T) {
	steps := []TextStep{
		{EventName: "code_changed", WorkspaceName: "doc", EmitEventName: "doc_updated"},
		{EventName: "code_changed", WorkspaceName: "blog", EmitEventName: "new_feature_added"},
		{EventName: "new_feature_added", WorkspaceName: "twitter"},
		{EventName: "new_feature_added", WorkspaceName: "hackernews"},
		{EventName: "new_feature_added", WorkspaceName: "linkedin"},
	}
	got := RenderWorkflowText("new_feature", "code_changed", steps)

	// Sorted within each event group, so output is stable and diffable.
	want := `workflow: new_feature
event: code_changed
- agent:blog
  - event:new_feature_added
    - agent:hackernews
    - agent:linkedin
    - agent:twitter
- agent:doc
  - event:doc_updated
`
	if got != want {
		t.Errorf("RenderWorkflowText mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderWorkflowTextRoundTrip(t *testing.T) {
	parsed, err := ParseWorkflowText(canonicalDoc)
	if err != nil {
		t.Fatalf("ParseWorkflowText: %v", err)
	}
	steps := FlattenWorkflowText(parsed)

	rendered := RenderWorkflowText(parsed.Name, parsed.StartEvent, steps)

	reparsed, err := ParseWorkflowText(rendered)
	if err != nil {
		t.Fatalf("rendered output must re-parse, got %v\n%s", err, rendered)
	}
	got := FlattenWorkflowText(reparsed)

	if len(got) != len(steps) {
		t.Fatalf("round trip changed step count: %d -> %d", len(steps), len(got))
	}
	// Compare as sets: rendering sorts, so order legitimately differs.
	original := make(map[TextStep]bool, len(steps))
	for _, s := range steps {
		original[s] = true
	}
	for _, s := range got {
		if !original[s] {
			t.Errorf("round trip produced a step that was not in the original: %+v", s)
		}
	}
}

func TestRenderWorkflowTextTerminatesOnCycle(t *testing.T) {
	// Cycles are rejected at setup, but the renderer must not hang if one ever
	// reaches storage another way.
	steps := []TextStep{
		{EventName: "a", WorkspaceName: "w1", EmitEventName: "b"},
		{EventName: "b", WorkspaceName: "w2", EmitEventName: "a"},
	}

	done := make(chan string, 1)
	go func() { done <- RenderWorkflowText("looped", "a", steps) }()

	select {
	case got := <-done:
		if !strings.Contains(got, "agent:w1") || !strings.Contains(got, "agent:w2") {
			t.Errorf("expected both agents in output, got:\n%s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RenderWorkflowText did not terminate on a cyclic graph")
	}
}

func TestRenderWorkflowTextEmpty(t *testing.T) {
	if got := RenderWorkflowText("w", "", nil); got != "workflow: w\n" {
		t.Errorf("a workflow with no start event should render just its name, got %q", got)
	}

	got := RenderWorkflowText("w", "start", nil)
	want := "workflow: w\nevent: start\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFlattenWorkflowTextDeduplicates(t *testing.T) {
	// A diamond: two agents emit the same event, so its consumers appear under
	// both branches but must produce one edge each.
	input := `workflow: w
event: start
- agent:a
  - event:mid
    - agent:shared
- agent:b
  - event:mid
    - agent:shared
`
	parsed, err := ParseWorkflowText(input)
	if err != nil {
		t.Fatalf("ParseWorkflowText: %v", err)
	}
	steps := FlattenWorkflowText(parsed)

	count := 0
	for _, s := range steps {
		if s.EventName == "mid" && s.WorkspaceName == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("mid->shared emitted %d times, want 1 (steps: %+v)", count, steps)
	}
}
