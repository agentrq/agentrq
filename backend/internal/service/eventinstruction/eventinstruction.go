// Package eventinstruction builds the "publish this event when you finish"
// text appended to a task body.
//
// Three call sites need this string — the REST createTask handler, and the
// event consumer's trigger and workflow-step paths — and they used to build it
// independently, which let the wording drift. Keeping it here means an agent
// sees the same contract however its task was created.
package eventinstruction

import "fmt"

// Params describes the publish a task should perform when it finishes.
type Params struct {
	// EventName is the event to publish. Required; without it there is no
	// instruction to give.
	EventName string
	// TaskID is the publishing task's own base62 ID. The agent echoes it back
	// so the server can identify the run from the task itself rather than
	// guessing from session state.
	//
	// This is the only run identifier the agent needs: the task row already
	// carries the workflow and the hop count, so naming the workflow as well
	// would be a second way to say the same thing — and a wrong or stale one
	// would be taken as a request to start a different run.
	TaskID string
	// PayloadGuidelines is optional prose from the event definition.
	PayloadGuidelines string
}

// Build returns the instruction to append to a task body, including the leading
// blank line.
//
// The call is spelled out completely — every argument the agent must not invent
// is written literally — because taskId is what identifies the run being
// continued. Leaving it implicit is what made a chain depend on the server
// guessing which task was publishing.
//
// Arguments are ordered fixed-then-free: name and taskId are copied verbatim,
// so they come first and are done with, leaving payload and faq — the only
// parts the agent actually composes — at the end where it can write freely
// without having to step back over values it must not alter.
func Build(p Params) string {
	if p.EventName == "" {
		return ""
	}

	args := fmt.Sprintf("name: %q", p.EventName)
	if p.TaskID != "" {
		args += fmt.Sprintf(", taskId: %q", p.TaskID)
	}
	args += ", payload: \"<what happened, in your words>\", faq: [{q, a}, ...]"

	// The call above lists every argument to pass, so arguments it omits need no
	// mention: spelling out what not to send would only name the thing the agent
	// should never have been thinking about.
	s := fmt.Sprintf("\n\n[On completion, before marking this task completed: call publishEvent(%s)]", args)
	s += "\nCopy name and taskId exactly as written above — taskId is what tells the server which run this continues."
	s += "\nWrite the payload yourself. faq is optional: question/answer pairs covering what subscribers are likely to ask —" +
		" tasks triggered by this event can render both."

	if p.PayloadGuidelines != "" {
		s += fmt.Sprintf("\nPayload guidelines: %s", p.PayloadGuidelines)
	}
	return s
}
