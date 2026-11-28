// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package hook

import (
	"context"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
)

// hooks returns the hook label of every telemetry event sent, by action.
func (f regFixture) hooks() map[entity.Action][]string {
	f.pub.mu.Lock()
	defer f.pub.mu.Unlock()
	out := map[entity.Action][]string{}
	for _, req := range f.pub.got {
		if e, ok := req.Event.(entity.CRUDEvent); ok {
			out[e.Action] = append(out[e.Action], e.Hook)
		}
	}
	return out
}

// A built-in hook's telemetry carries its name, from the registration on.
func TestTelemetryNamesABuiltInHook(t *testing.T) {
	f := qSetup(t)
	f.pub.got = nil
	f.receive(t)
	f.ctl().drain(context.Background())
	got := f.hooks()
	if len(got[entity.ActionHookReceive]) != 1 || got[entity.ActionHookReceive][0] != "github" {
		t.Errorf("hook_receive was sent with hooks %v, want [github]", got[entity.ActionHookReceive])
	}

	f = qSetup(t)
	f.pub.got = nil
	f.run.res = hookrunner.Result{Accept: false, Reason: "bad signature"}
	f.receive(t)
	f.ctl().drain(context.Background())
	got = f.hooks()
	if got[entity.ActionHookReject][0] != "github" || got[entity.ActionHookReceive][0] != "github" {
		t.Errorf("a rejected request was sent with hooks %v", got)
	}
}

// An account's own hook is "custom": its name is the account's to choose.
func TestTelemetryCallsAnOwnHookCustom(t *testing.T) {
	f := regSetup(t, true)
	_, err := f.c.CreateRegistration(context.Background(), user, entity.HookRegistrationRequest{
		HookID: f.own, Name: ptr("mine"), Inputs: map[string]any{"webhookSecret": "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.hooks()[entity.ActionHookRegister]; len(got) != 1 || got[0] != entity.HookLabelCustom {
		t.Errorf("hook_register was sent with hooks %v, want [custom]", got)
	}
}

// A restarted instance knows a registration's hook once it reads it: by
// processing one of its requests, or by showing it.
func TestTelemetryLearnsTheHookWhereItIsRead(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	restart := func() {
		f.c = New(Params{Repository: f.repo, Storage: f.blobs, Runner: f.run, IDGen: &ids{n: 5000}, Events: f.events, PubSub: f.pub,
			TokenKey: key, BaseURL: "https://rq.example", Now: func() time.Time { return *f.clock }})
	}

	restart()
	if got := f.ctl().hookLabel(f.reg.ID); got != entity.HookLabelUnknown {
		t.Fatalf("a fresh instance labels the registration %q, want unknown", got)
	}
	f.receive(t)
	f.ctl().drain(ctx)
	if got := f.ctl().hookLabel(f.reg.ID); got != "github" {
		t.Errorf("after processing a request, the label is %q, want github", got)
	}

	restart()
	if _, err := f.c.GetRegistration(ctx, user, f.reg.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.ctl().hookLabel(f.reg.ID); got != "github" {
		t.Errorf("after showing the registration, the label is %q, want github", got)
	}

	// A hook gone is not remembered as anything.
	restart()
	f.ctl().rememberHook(f.reg.ID, model.Hook{})
	if got := f.ctl().hookLabel(f.reg.ID); got != entity.HookLabelUnknown {
		t.Errorf("a missing hook is labelled %q, want unknown", got)
	}
}

// Counting an action reads nothing: the label comes from memory, and a
// registration not in it is "unknown" rather than looked up.
func TestTelemetryReadsNothing(t *testing.T) {
	f := regSetup(t, false)
	reg := f.create(t)
	f.pub.got = nil
	c := f.c.(*controller)
	c.repo = nil // any read would panic
	c.received(context.Background(), model.HookDelivery{UserID: user, RegistrationID: reg.ID}, model.HookDeliveryPublished)
	c.received(context.Background(), model.HookDelivery{UserID: user, RegistrationID: reg.ID + 1}, model.HookDeliveryFailed)
	if got := f.hooks()[entity.ActionHookReceive]; len(got) != 2 || got[0] != "github" || got[1] != entity.HookLabelUnknown {
		t.Errorf("hook_receive was sent with hooks %v, want [github unknown]", got)
	}
}

// A registration deleted is forgotten; one not deleted is kept.
func TestTelemetryForgetsADeletedRegistration(t *testing.T) {
	ctx := context.Background()
	f := regSetup(t, false)
	reg := f.create(t)
	c := f.c.(*controller)
	if err := c.DeleteRegistration(ctx, user+1, reg.ID); err == nil {
		t.Fatal("another account deleted the registration")
	}
	if got := c.hookLabel(reg.ID); got != "github" {
		t.Errorf("after a refused delete the label is %q, want github", got)
	}
	if err := c.DeleteRegistration(ctx, user, reg.ID); err != nil {
		t.Fatal(err)
	}
	if got := c.hookLabel(reg.ID); got != entity.HookLabelUnknown {
		t.Errorf("after the delete the label is %q, want unknown", got)
	}
}
