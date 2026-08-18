package eventinstruction

import (
	"strings"
	"testing"
)

func TestBuildNamesTheEventAndTask(t *testing.T) {
	got := Build(Params{EventName: "blog_published", TaskID: "0hGlYcRCJTV"})

	for _, want := range []string{
		`name: "blog_published"`,
		`taskId: "0hGlYcRCJTV"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instruction missing %q\ngot: %s", want, got)
		}
	}
}

// The copy-exactly arguments come first so the agent is done with them before
// it starts composing; the parts it writes itself trail the call.
func TestBuildPutsFixedArgumentsBeforeAuthoredOnes(t *testing.T) {
	got := Build(Params{EventName: "blog_published", TaskID: "0hGlYcRCJTV"})

	taskID := strings.Index(got, "taskId:")
	payload := strings.Index(got, "payload:")
	faq := strings.Index(got, "faq:")

	if taskID == -1 || payload == -1 || faq == -1 {
		t.Fatalf("expected name, taskId, payload and faq in the call\ngot: %s", got)
	}
	if !(taskID < payload && payload < faq) {
		t.Errorf("expected taskId before payload before faq\ngot: %s", got)
	}
}

// taskId already carries the workflow and hop count, so a workflow argument
// would be a second, conflicting way to name the run. The call lists everything
// to pass, so the instruction simply never raises the subject — mentioning it,
// even to forbid it, would put it back in front of the agent.
func TestBuildNeverMentionsWorkflow(t *testing.T) {
	got := Build(Params{EventName: "code_changed", TaskID: "0hGlYcRCJTV"})

	if strings.Contains(strings.ToLower(got), "workflow") {
		t.Errorf("instruction should not raise the workflow argument at all\ngot: %s", got)
	}
}

// The payload and faq are the only things the agent supplies, so both have to
// be asked for explicitly — faq going unmentioned is why {{EVENT_FAQ}} rendered
// empty for subscribers.
func TestBuildAsksForPayloadAndFAQ(t *testing.T) {
	got := Build(Params{EventName: "code_changed"})

	if !strings.Contains(got, "faq:") {
		t.Errorf("instruction should offer the faq argument\ngot: %s", got)
	}
	if !strings.Contains(got, "payload:") {
		t.Errorf("instruction should mark where the payload goes\ngot: %s", got)
	}
}

func TestBuildAppendsPayloadGuidelinesOnlyWhenPresent(t *testing.T) {
	with := Build(Params{EventName: "code_changed", PayloadGuidelines: "list the changed files"})
	if !strings.Contains(with, "Payload guidelines: list the changed files") {
		t.Errorf("guidelines should be appended\ngot: %s", with)
	}

	without := Build(Params{EventName: "code_changed"})
	if strings.Contains(without, "Payload guidelines") {
		t.Errorf("no guidelines means no guidelines line\ngot: %s", without)
	}
}

// Callers append the result to a task body unconditionally, so an event-less
// task must not gain a stray heading.
func TestBuildWithoutEventReturnsNothing(t *testing.T) {
	if got := Build(Params{TaskID: "0hGlYcRCJTV"}); got != "" {
		t.Errorf("expected empty string, got: %s", got)
	}
}
