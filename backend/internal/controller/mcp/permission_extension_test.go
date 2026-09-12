// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package mcp

import (
	"context"
	"errors"
	"testing"
)

// An extension's verdict is a real answer, and is not a person's.
//
// This is the whole reason SendPermissionVerdictFrom exists. Delivered through
// the human entry point an extension's decision would be counted under
// `permission_manual_allow` — the one number on the analytics screen that is
// supposed to mean somebody stopped and thought about it.
func TestExtensionVerdictIsNotCountedAsAManualApproval(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-x1"] = "sess-1"

	_ = ps.SendPermissionVerdictFrom(context.Background(), 42, "req-x1", "allow", "guardrail")

	if got := rec.count("permission_extension_allow"); got != 1 {
		t.Errorf("expected 1 extension approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("nobody clicked anything, got %d manual (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_auto_allow"); got != 0 {
		t.Errorf("an extension is not an auto-allow rule either, got %d (all: %v)", got, rec.snapshot())
	}
}

// The same for the direction that costs nothing but a tool call.
func TestExtensionDenialIsCountedAsAnExtensionDenial(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-x2"] = "sess-1"

	_ = ps.SendPermissionVerdictFrom(context.Background(), 42, "req-x2", "deny", "guardrail")

	if got := rec.count("permission_extension_deny"); got != 1 {
		t.Errorf("expected 1 extension denial, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_manual_deny"); got != 0 {
		t.Errorf("nobody clicked anything, got %d manual (all: %v)", got, rec.snapshot())
	}
}

// An empty decider is the browser, not a nameless extension.
//
// Every verdict from the web UI arrives with the field absent, so this is the
// overwhelmingly common path through the new entry point and it has to behave
// exactly as it did before the field existed.
func TestVerdictWithNoDeciderIsStillAManualOne(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-x3"] = "sess-1"

	_ = ps.SendPermissionVerdictFrom(context.Background(), 42, "req-x3", "allow", "")

	if got := rec.count("permission_manual_allow"); got != 1 {
		t.Errorf("expected 1 manual approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_extension_allow"); got != 0 {
		t.Errorf("an unnamed decider is the human, got %d extension (all: %v)", got, rec.snapshot())
	}
}

// An extension may answer this request. It may not write a standing rule.
//
// "allow_always" does two things, and only the first of them was consented to:
// the rule it writes answers every future request without asking anybody, and
// it stays behind when the extension is uninstalled. Refused rather than
// downgraded to a plain allow — an extension that asked for the wrong thing
// should be told so, not quietly given something else.
func TestExtensionCannotWriteAnAutoAllowRule(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-x4"] = "sess-1"
	ps.requestTools["req-x4"] = "Bash"
	ps.requestParams["req-x4"] = &PermissionRequestParams{
		RequestID:    "req-x4",
		ToolName:     "Bash",
		InputPreview: `{"command":"rm -rf /"}`,
	}

	err := ps.SendPermissionVerdictFrom(context.Background(), 42, "req-x4", "allow_always", "guardrail")

	if !errors.Is(err, ErrExtensionCannotRemember) {
		t.Fatalf("expected the standing rule refused, got %v", err)
	}
	if len(ps.autoAllowedTools) != 0 {
		t.Errorf("no rule should have been written, got %v", ps.autoAllowedTools)
	}
	if got := len(rec.snapshot()); got != 0 {
		t.Errorf("a refused verdict decides nothing and counts as nothing, got %v", rec.snapshot())
	}
}

// `decidedBy` is drawn in the task feed as the thing that made a decision, and
// it arrives in an HTTP body. A caller is already authenticated and already
// allowed to answer the request, so this is not about privilege — it is that an
// identifier written into the feed has to be an identifier.
func TestVerdictFromSomethingThatIsNotAnExtensionNameIsRefused(t *testing.T) {
	for _, name := range []string{"You", "not a name", "<b>ops</b>", "-leading", "trailing-", "UPPER"} {
		t.Run(name, func(t *testing.T) {
			ps, rec := approvalServer(t, nil)
			ps.permissionRequests["req-x5"] = "sess-1"

			err := ps.SendPermissionVerdictFrom(context.Background(), 42, "req-x5", "allow", name)

			if !errors.Is(err, ErrBadDecider) {
				t.Fatalf("expected %q refused as a decider, got %v", name, err)
			}
			if got := len(rec.snapshot()); got != 0 {
				t.Errorf("a refused verdict counts as nothing, got %v", rec.snapshot())
			}
		})
	}
}

// The message the user is looking at says who answered it.
//
// Without this the feed shows "Allowed" on a card nobody clicked, which is the
// one outcome this whole feature must not produce: a decision the user is
// invited to remember making.
func TestExtensionVerdictRecordsTheDeciderOnTheMessage(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	sessID := connectedServer(t, ps, "agent")

	ps.permissionRequests["req-x6"] = sessID
	ps.permissionResponses["req-x6"] = 700
	ps.requestTaskIDs["req-x6"] = 42

	var written map[string]any
	ps.updateMessageMetadata = func(ctx context.Context, taskID, messageID int64, metadata any) error {
		written, _ = metadata.(map[string]any)
		return nil
	}

	if err := ps.SendPermissionVerdictFrom(context.Background(), 42, "req-x6", "deny", "guardrail"); err != nil {
		t.Fatalf("expected the verdict delivered, got %v", err)
	}

	if written["status"] != "deny" {
		t.Errorf("expected the verdict recorded, got %v", written["status"])
	}
	if written["decidedBy"] != "guardrail" {
		t.Errorf("expected the deciding extension recorded, got %v", written["decidedBy"])
	}
}

// A verdict the user gave leaves no decider behind.
//
// An absent field is how "you did this" is said, and every message written
// before extensions existed says it that way. Writing `decidedBy: "you"` would
// be a value nothing else in the system produces.
func TestHumanVerdictRecordsNoDecider(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	sessID := connectedServer(t, ps, "agent")

	ps.permissionRequests["req-x7"] = sessID
	ps.permissionResponses["req-x7"] = 700
	ps.requestTaskIDs["req-x7"] = 42

	var written map[string]any
	ps.updateMessageMetadata = func(ctx context.Context, taskID, messageID int64, metadata any) error {
		written, _ = metadata.(map[string]any)
		return nil
	}

	if err := ps.SendPermissionVerdict(context.Background(), 42, "req-x7", "allow"); err != nil {
		t.Fatalf("expected the verdict delivered, got %v", err)
	}

	if _, ok := written["decidedBy"]; ok {
		t.Errorf("a human verdict should name no decider, got %v", written["decidedBy"])
	}
}
