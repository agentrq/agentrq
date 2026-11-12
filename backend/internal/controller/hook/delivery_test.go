// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"slices"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/mustafaturan/monoflake"
)

func (r *repo) GetHookRegistrationByID(ctx context.Context, id int64) (model.HookRegistration, error) {
	if err := r.err("GetHookRegistrationByID"); err != nil {
		return model.HookRegistration{}, err
	}
	return r.Repository.GetHookRegistrationByID(ctx, id)
}

func (r *repo) CreateHookDelivery(ctx context.Context, d model.HookDelivery) error {
	if err := r.err("CreateHookDelivery"); err != nil {
		return err
	}
	return r.Repository.CreateHookDelivery(ctx, d)
}

func (r *repo) CountQueuedHookDeliveries(ctx context.Context, id int64) (int64, error) {
	if err := r.err("CountQueuedHookDeliveries"); err != nil {
		return 0, err
	}
	return r.Repository.CountQueuedHookDeliveries(ctx, id)
}

func (r *repo) ListDueHookDeliveries(ctx context.Context, now time.Time, limit int) ([]model.HookDelivery, error) {
	if err := r.err("ListDueHookDeliveries"); err != nil {
		return nil, err
	}
	return r.Repository.ListDueHookDeliveries(ctx, now, limit)
}

func (r *repo) ClaimHookDelivery(ctx context.Context, id int64, now, until time.Time) (bool, error) {
	if err := r.err("ClaimHookDelivery"); err != nil {
		return false, err
	}
	return r.Repository.ClaimHookDelivery(ctx, id, now, until)
}

func (r *repo) GetHookDelivery(ctx context.Context, id int64) (model.HookDelivery, error) {
	if err := r.err("GetHookDelivery"); err != nil {
		return model.HookDelivery{}, err
	}
	return r.Repository.GetHookDelivery(ctx, id)
}

func (r *repo) UpdateHookDelivery(ctx context.Context, d model.HookDelivery) error {
	if err := r.err("UpdateHookDelivery"); err != nil {
		return err
	}
	return r.Repository.UpdateHookDelivery(ctx, d)
}

func (r *repo) RouteHookDelivery(ctx context.Context, d model.HookDelivery, events []model.HookPendingEvent) ([]model.HookPendingEvent, error) {
	if err := r.err("RouteHookDelivery"); err != nil {
		return nil, err
	}
	return r.Repository.RouteHookDelivery(ctx, d, events)
}

func (r *repo) ListHookDeliveries(ctx context.Context, regID, userID, before int64, limit int) ([]model.HookDelivery, error) {
	if err := r.err("ListHookDeliveries"); err != nil {
		return nil, err
	}
	return r.Repository.ListHookDeliveries(ctx, regID, userID, before, limit)
}

func (r *repo) DeleteHookDeliveriesBefore(ctx context.Context, before time.Time) error {
	if err := r.err("DeleteHookDeliveriesBefore"); err != nil {
		return err
	}
	return r.Repository.DeleteHookDeliveriesBefore(ctx, before)
}

// fakeRunner loads the module, as the real one does on a miss, and answers
// with res and err.
type fakeRunner struct {
	inspector
	res   hookrunner.Result
	err   error
	calls int
	got   hookrunner.HandleInput
	wasm  []byte
	// during runs once, inside the first call, as a slow run would let
	// another worker act.
	during func()
}

func (f *fakeRunner) Handle(ctx context.Context, mod hookrunner.Module, _ hookrunner.Manifest, in hookrunner.HandleInput) (hookrunner.Result, error) {
	f.calls++
	f.got = in
	wasm, err := mod.Load(ctx)
	if err != nil {
		return hookrunner.Result{}, err
	}
	f.wasm = wasm
	if during := f.during; during != nil {
		f.during = nil
		during()
	}
	return f.res, f.err
}

type qFixture struct {
	regFixture
	run   *fakeRunner
	clock *time.Time
	reg   entity.HookRegistration
	token string
}

var start = time.Date(2026, 11, 12, 7, 0, 0, 0, time.UTC)

func qSetup(t *testing.T) qFixture {
	t.Helper()
	f := regSetup(t, false)
	run := &fakeRunner{inspector: modules, res: hookrunner.Result{Accept: true}}
	clock := start
	f.c = New(Params{Repository: f.repo, Storage: f.blobs, Runner: run, IDGen: &ids{n: 1000}, Events: f.events, PubSub: f.pub,
		TokenKey: key, BaseURL: "https://rq.example", DeliveryRetention: 24 * time.Hour, Now: func() time.Time { return clock }})
	reg := f.create(t)
	f.pub.got = nil
	return qFixture{regFixture: f, run: run, clock: &clock, reg: reg, token: reg.URLWithToken[strings.LastIndex(reg.URLWithToken, "=")+1:]}
}

func (f qFixture) ctl() *controller { return f.c.(*controller) }

func (f qFixture) incoming(token string) entity.HookIncoming {
	return entity.HookIncoming{RegistrationID: f.reg.ID, Token: token, Peer: "203.0.113.9", Method: "POST", Path: "/api/v1/hooks/in/x",
		Query: map[string][]string{"a": {"1"}}, Headers: map[string][]string{"x-github-event": {"workflow_run"}}, Body: []byte(`{"ok":true}`)}
}

func (f qFixture) receive(t *testing.T) int64 {
	t.Helper()
	id, err := f.c.Receive(context.Background(), f.incoming(f.token))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f qFixture) delivery(t *testing.T, id int64) model.HookDelivery {
	t.Helper()
	d, err := f.repo.Repository.GetHookDelivery(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// published returns what went out on the events topic, and the telemetry
// actions sent.
func (f qFixture) published() (events []entity.EventPublishedPayload, actions []entity.Action) {
	f.pub.mu.Lock()
	defer f.pub.mu.Unlock()
	for _, req := range f.pub.got {
		switch e := req.Event.(type) {
		case entity.EventPublishedPayload:
			events = append(events, e)
		case entity.CRUDEvent:
			actions = append(actions, e.Action)
		}
	}
	return events, actions
}

func TestReceiveQueuesARequest(t *testing.T) {
	f := qSetup(t)
	id := f.receive(t)
	d := f.delivery(t, id)
	var req hookrunner.Request
	_ = json.Unmarshal(d.Request, &req)
	if d.Status != model.HookDeliveryQueued || d.UserID != user || d.RegistrationID != f.reg.ID || !d.NextAttemptAt.Equal(start) ||
		req.Method != "POST" || string(req.Body) != `{"ok":true}` || req.Headers["x-github-event"][0] != "workflow_run" ||
		d.BodySHA256 != "4062edaf750fb8074e7e83e0c9028c94e32468a8b6f1614774328ef045150f93" || d.EventNames != "[]" {
		t.Errorf("delivery = %+v, request %+v", d, req)
	}
	if _, actions := f.published(); !slices.Equal(actions, []entity.Action{entity.ActionHookReceive}) {
		t.Errorf("telemetry = %v", actions)
	}
	select {
	case <-f.ctl().q.wake:
	default:
		t.Error("the workers were not woken")
	}
	// A second request finds the token remembered, and wakes nothing twice.
	f.receive(t)
	f.receive(t)
	if len(f.ctl().q.verified) != 1 || len(f.ctl().q.wake) != 1 {
		t.Errorf("verified %d, wake %d", len(f.ctl().q.verified), len(f.ctl().q.wake))
	}
}

func TestReceiveAnswersEveryRefusalAlike(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	other := f.incoming(f.token)
	other.RegistrationID = 999
	if _, err := f.c.Receive(ctx, other); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("no registration = %v", err)
	}
	if _, err := f.c.Receive(ctx, f.incoming("wrongtkn")); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("wrong token = %v", err)
	}
	if _, err := f.c.UpdateRegistration(ctx, user, f.reg.ID, entity.HookRegistrationRequest{Enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.Receive(ctx, f.incoming(f.token)); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("turned off = %v", err)
	}

	big := f.incoming(f.token)
	big.Body = make([]byte, MaxRequestBodySize+1)
	_, err := f.c.Receive(ctx, big)
	wantHookError(t, err, entity.HookTooLarge, "over 1048576 bytes")
}

func TestReceiveComparesAHashForEveryRefusal(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	var hashes [][]byte
	compareHash = func(hash, token []byte) error {
		hashes = append(hashes, hash)
		return bcrypt.CompareHashAndPassword(hash, token)
	}
	t.Cleanup(func() { compareHash = bcrypt.CompareHashAndPassword })

	unknown := f.incoming(f.token)
	unknown.RegistrationID = 999
	_, _ = f.c.Receive(ctx, unknown)
	_, _ = f.c.Receive(ctx, f.incoming("wrongtkn"))
	if _, err := f.c.UpdateRegistration(ctx, user, f.reg.ID, entity.HookRegistrationRequest{Enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	_, _ = f.c.Receive(ctx, f.incoming(f.token))

	stored := mustGet(t, f.regFixture, f.reg.ID).TokenHash
	want := []string{string(dummyHash()), stored, string(dummyHash())}
	if len(hashes) != len(want) {
		t.Fatalf("%d compares, want %d", len(hashes), len(want))
	}
	for i, h := range hashes {
		if string(h) != want[i] {
			t.Errorf("compare %d used the wrong hash", i)
		}
	}
	if cost, err := bcrypt.Cost(dummyHash()); err != nil || cost != bcrypt.DefaultCost {
		t.Errorf("dummy hash cost = %d, %v", cost, err)
	}
}

func TestReceiveThrottlesWrongTokens(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	for range maxTokenFailures - 1 {
		if _, err := f.c.Receive(ctx, f.incoming("wrongtkn")); !errors.Is(err, base.ErrNotFound) {
			t.Fatal(err)
		}
	}
	// The right token still passes until the limit is reached.
	f.receive(t)
	if _, err := f.c.Receive(ctx, f.incoming("wrongtkn")); !errors.Is(err, base.ErrNotFound) {
		t.Fatal(err)
	}
	_, err := f.c.Receive(ctx, f.incoming(f.token))
	wantHookError(t, err, entity.HookThrottled, "wrong token")

	// Another address is not held to this one's failures.
	in := f.incoming(f.token)
	in.Peer = "198.51.100.1"
	if _, err := f.c.Receive(ctx, in); err != nil {
		t.Errorf("another address = %v", err)
	}
	// A minute on, the address may try again.
	*f.clock = f.clock.Add(time.Minute)
	f.receive(t)
}

func TestReceiveThrottlesARegistration(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	for range maxRegistrationRequests {
		f.receive(t)
	}
	_, err := f.c.Receive(ctx, f.incoming(f.token))
	wantHookError(t, err, entity.HookThrottled, "too many requests to this hook URL")

	// A backlog: the minute passed, but too many are waiting.
	*f.clock = f.clock.Add(time.Minute)
	for i := range maxQueued - maxRegistrationRequests {
		d := model.HookDelivery{ID: int64(10000 + i), RegistrationID: f.reg.ID, UserID: user, ReceivedAt: start, Status: model.HookDeliveryQueued,
			NextAttemptAt: start, BodySHA256: "0", EventNames: "[]"}
		if err := f.repo.db.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.c.Receive(ctx, f.incoming(f.token))
	wantHookError(t, err, entity.HookThrottled, "are waiting")
}

func TestReceiveFails(t *testing.T) {
	ctx := context.Background()
	for _, method := range []string{"GetHookRegistrationByID", "CountQueuedHookDeliveries", "CreateHookDelivery"} {
		f := qSetup(t)
		f.repo.fail[method] = errBoom
		if _, err := f.c.Receive(ctx, f.incoming(f.token)); !errors.Is(err, errBoom) {
			t.Errorf("%s: err = %v", method, err)
		}
	}
}

func TestTokenMatchesForgetsWhenFull(t *testing.T) {
	f := qSetup(t)
	c := f.ctl()
	hash := mustGet(t, f.regFixture, f.reg.ID).TokenHash
	for i := range verifiedCap {
		c.q.verified[[32]byte{byte(i), byte(i >> 8)}] = struct{}{}
	}
	if !c.tokenMatches(hash, f.token) || len(c.q.verified) != 1 {
		t.Errorf("verified = %d", len(c.q.verified))
	}
	// A rotated token's hash is another key: the old token no longer passes.
	if _, err := f.c.RotateToken(context.Background(), user, f.reg.ID); err != nil {
		t.Fatal(err)
	}
	if c.tokenMatches(mustGet(t, f.regFixture, f.reg.ID).TokenHash, f.token) {
		t.Error("the old token still passes")
	}
}

func TestDeliveryPublished(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.run.res = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{
		{Name: "ci.failed", DedupKey: "run-1", Payload: `{"run":1}`, FAQ: []hookrunner.FAQ{{Q: "which branch?", A: "main"}}},
		{Name: "push", Payload: "pushed"},
		{Name: "ci.failed", DedupKey: "run-1", Payload: "the same run, sent twice"},
	}}
	id := f.receive(t)
	f.ctl().drain(ctx)

	d := f.delivery(t, id)
	if d.Status != model.HookDeliveryPublished || d.Request != nil || d.Pending != nil || d.EventNames != `["ci.failed","push"]` || d.Attempts != 1 || d.Error != "" {
		t.Errorf("delivery = %+v", d)
	}
	if f.run.got.RegistrationID != monoflake.ID(f.reg.ID).String() || f.run.got.Inputs["webhookSecret"] != "s3cret" ||
		string(f.run.got.Request.Body) != `{"ok":true}` || !f.run.got.Now.Equal(start) || string(f.run.wasm) != "github-1" {
		t.Errorf("hook input = %+v, module %q", f.run.got, f.run.wasm)
	}
	events, actions := f.published()
	if len(events) != 2 || events[0].EventID != ciFailed || events[0].Name != "event-500" || events[0].Payload != `{"run":1}` ||
		events[0].FAQ[0].A != "main" || events[1].EventID != pushed || events[1].Payload != "pushed" {
		t.Errorf("published = %+v", events)
	}
	if !slices.Equal(actions, []entity.Action{entity.ActionHookReceive, entity.ActionHookPublish}) {
		t.Errorf("telemetry = %v", actions)
	}

	// The provider sends it again: the keyed event is a duplicate, the
	// other is published again.
	id = f.receive(t)
	f.ctl().drain(ctx)
	if d = f.delivery(t, id); d.Status != model.HookDeliveryPublished || d.EventNames != `["push"]` {
		t.Errorf("retry = %+v", d)
	}
	f.run.res.Events = f.run.res.Events[:1]
	id = f.receive(t)
	f.ctl().drain(ctx)
	if d = f.delivery(t, id); d.Status != model.HookDeliveryDuplicate || d.Request != nil {
		t.Errorf("duplicate = %+v", d)
	}
}

func TestDeliveryDecidedByTheHook(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		res    hookrunner.Result
		status int16
		reason string
		action entity.Action
	}{
		"rejected":         {hookrunner.Result{Reason: "bad signature"}, model.HookDeliveryRejected, "bad signature", entity.ActionHookReject},
		"nothing to fire":  {hookrunner.Result{Accept: true, Reason: "a ping"}, model.HookDeliveryIgnored, "a ping", 0},
		"nothing routed":   {hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "release.published"}}}, model.HookDeliveryIgnored, "none of its events is routed", 0},
		"its reason first": {hookrunner.Result{Accept: true, Reason: "filtered", Events: []hookrunner.EventOut{{Name: "release.published"}}}, model.HookDeliveryIgnored, "filtered", 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := qSetup(t)
			f.run.res = tc.res
			id := f.receive(t)
			f.ctl().drain(ctx)
			d := f.delivery(t, id)
			if d.Status != tc.status || d.Error != tc.reason || d.Request != nil {
				t.Errorf("delivery = %+v", d)
			}
			events, actions := f.published()
			if len(events) != 0 || (tc.action != 0) != slices.Contains(actions, tc.action) {
				t.Errorf("published %v, telemetry %v", events, actions)
			}
		})
	}
}

func TestDeliveryFailsAtOnce(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		setup  func(f qFixture)
		reason string
	}{
		"a wrong answer": {func(f qFixture) { f.run.err = &hookrunner.OutputError{} }, "hook output"},
		"its hook off": {func(f qFixture) {
			if _, err := f.c.SetBuiltInEnabled(ctx, f.github, false); err != nil {
				panic(err)
			}
		}, "its hook is gone or turned off"},
		"its registration off": {func(f qFixture) {
			if _, err := f.c.UpdateRegistration(ctx, user, f.reg.ID, entity.HookRegistrationRequest{Enabled: ptr(false)}); err != nil {
				panic(err)
			}
		}, "its registration is turned off"},
		"its inputs unreadable": {func(f qFixture) { f.ctl().tokenKey = strings.Repeat("k", 32) }, "its inputs cannot be read"},
	} {
		t.Run(name, func(t *testing.T) {
			f := qSetup(t)
			id := f.receive(t)
			tc.setup(f)
			f.ctl().drain(ctx)
			d := f.delivery(t, id)
			if d.Status != model.HookDeliveryFailed || !strings.Contains(d.Error, tc.reason) || d.Request == nil || d.Attempts != 1 {
				t.Fatalf("delivery = %+v", d)
			}
			ds, err := f.c.ListDeliveries(ctx, user, f.reg.ID, 0, 10)
			if err != nil || len(ds) != 1 || ds[0].Status != "failed" || !ds[0].Replayable || ds[0].EventNames == nil {
				t.Errorf("listed = %+v, %v", ds, err)
			}
		})
	}
}

func TestDeliveryWaitingOnARetryFailsOnceItsRegistrationIsOff(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.run.err = errBoom
	id := f.receive(t)
	f.ctl().drain(ctx)
	if d := f.delivery(t, id); d.Status != model.HookDeliveryQueued || d.Attempts != 1 {
		t.Fatalf("first attempt = %+v", d)
	}
	if _, err := f.c.UpdateRegistration(ctx, user, f.reg.ID, entity.HookRegistrationRequest{Enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	f.run.err = nil
	calls := f.run.calls
	*f.clock = f.clock.Add(backoff[0])
	f.ctl().drain(ctx)
	d := f.delivery(t, id)
	if d.Status != model.HookDeliveryFailed || d.Error != "its registration is turned off" || d.Attempts != 2 || d.Request == nil {
		t.Errorf("after turning it off = %+v", d)
	}
	if f.run.calls != calls {
		t.Error("the hook ran for a turned-off registration")
	}
	if events, _ := f.published(); len(events) != 0 {
		t.Errorf("published %v", events)
	}
}

func TestReplayRefusedWhileTheRegistrationIsOff(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.run.err = &hookrunner.OutputError{}
	id := f.receive(t)
	f.ctl().drain(ctx)
	if _, err := f.c.UpdateRegistration(ctx, user, f.reg.ID, entity.HookRegistrationRequest{Enabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	err := f.c.ReplayDelivery(ctx, user, f.reg.ID, id)
	wantHookError(t, err, entity.HookConflict, "turned off")
	if d := f.delivery(t, id); d.Status != model.HookDeliveryFailed || d.Attempts != 1 {
		t.Errorf("refused replay changed the delivery: %+v", d)
	}
	if err := f.c.ReplayDelivery(ctx, user+1, f.reg.ID, id); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account's registration = %v", err)
	}

	// Turned on again, it replays.
	if _, err := f.c.UpdateRegistration(ctx, user, f.reg.ID, entity.HookRegistrationRequest{Enabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := f.c.ReplayDelivery(ctx, user, f.reg.ID, id); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryRetriedThenFailed(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.run.err = errBoom
	id := f.receive(t)
	for i, wait := range backoff {
		f.ctl().drain(ctx)
		d := f.delivery(t, id)
		if d.Status != model.HookDeliveryQueued || d.Attempts != int16(i+1) || d.Error != "boom" || !d.NextAttemptAt.Equal(f.clock.Add(wait)) || d.Request == nil {
			t.Fatalf("attempt %d = %+v", i+1, d)
		}
		// Not due yet: nothing runs.
		calls := f.run.calls
		f.ctl().drain(ctx)
		if f.run.calls != calls {
			t.Fatalf("attempt %d ran before its time", i+1)
		}
		*f.clock = f.clock.Add(wait)
	}
	f.ctl().drain(ctx)
	if d := f.delivery(t, id); d.Status != model.HookDeliveryFailed || d.Attempts != int16(len(backoff)+1) || d.Error != "boom" {
		t.Errorf("last = %+v", d)
	}

	// Replayed, it runs again, from its first attempt.
	f.run.err = nil
	if err := f.c.ReplayDelivery(ctx, user, f.reg.ID, id); err != nil {
		t.Fatal(err)
	}
	f.ctl().drain(ctx)
	if d := f.delivery(t, id); d.Status != model.HookDeliveryIgnored || d.Attempts != 1 {
		t.Errorf("replayed = %+v", d)
	}
	if err := f.c.ReplayDelivery(ctx, user, f.reg.ID, id); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("replay of an ignored delivery = %v", err)
	}
}

func TestDeliveryRetriedOnAnInstanceError(t *testing.T) {
	ctx := context.Background()
	for name, setup := range map[string]func(f qFixture){
		"reading the registration": func(f qFixture) { f.repo.fail["GetHookRegistrationByID"] = errBoom },
		"reading the hook":         func(f qFixture) { f.repo.fail["GetHook"] = errBoom },
		"loading the module":       func(f qFixture) { clear(f.blobs.m) },
		"routing":                  func(f qFixture) { f.repo.fail["RouteHookDelivery"] = errBoom },
	} {
		t.Run(name, func(t *testing.T) {
			f := qSetup(t)
			f.run.res = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push"}}}
			id := f.receive(t)
			setup(f)
			f.ctl().drain(ctx)
			if d := f.delivery(t, id); d.Status != model.HookDeliveryQueued || d.Attempts != 1 || d.Error != "boom" || d.Request == nil {
				t.Errorf("delivery = %+v", d)
			}
		})
	}
}

// A run that outlasts its lease loses the delivery to the worker that claims
// it next: its outcome is not written, and its events are not published again.
func TestDeliveryOutlastingItsLease(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		res hookrunner.Result
		err error
	}{
		"routed":  {res: hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push", Payload: "p"}}}},
		"decided": {res: hookrunner.Result{Reason: "bad signature"}},
		"retried": {err: errBoom},
		"failed":  {err: &hookrunner.OutputError{}},
	} {
		t.Run(name, func(t *testing.T) {
			f := qSetup(t)
			f.run.res, f.run.err = tc.res, tc.err
			id := f.receive(t)
			// Another worker claims it once the lease is over, and routes it.
			f.run.during = func() {
				*f.clock = f.clock.Add(lease)
				res, err := f.run.res, f.run.err
				f.run.res, f.run.err = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push", Payload: "p"}}}, nil
				f.ctl().drain(ctx)
				f.run.res, f.run.err = res, err
			}
			f.ctl().drain(ctx)

			d := f.delivery(t, id)
			if d.Status != model.HookDeliveryPublished || d.Attempts != 2 || d.Error != "" {
				t.Errorf("delivery = %+v", d)
			}
			if events, _ := f.published(); len(events) != 1 {
				t.Errorf("published %d times", len(events))
			}
		})
	}
}

// A routed delivery whose worker died before publishing is published when
// its lease ends; one whose publish could not finish stays routed for that.
func TestRoutedDeliveryRecovered(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.run.res = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push", Payload: "p"}, {Name: "ci.failed", Payload: "c"}}}
	id := f.receive(t)

	f.events.getErr = errBoom
	f.ctl().drain(ctx)
	d := f.delivery(t, id)
	if d.Status != model.HookDeliveryRouted || d.Pending == nil || !d.NextAttemptAt.Equal(start.Add(lease)) {
		t.Fatalf("delivery = %+v", d)
	}
	if events, _ := f.published(); len(events) != 0 {
		t.Fatalf("published half: %v", events)
	}

	// Its lease not yet over, nothing happens; after it, it is published.
	f.events.getErr = nil
	f.ctl().drain(ctx)
	if events, _ := f.published(); len(events) != 0 {
		t.Fatal("published before the lease ended")
	}
	*f.clock = f.clock.Add(lease)
	f.repo.fail["UpdateHookDelivery"] = errBoom
	f.ctl().drain(ctx)
	if d = f.delivery(t, id); d.Status != model.HookDeliveryRouted {
		t.Fatalf("not marked = %+v", d)
	}
	delete(f.repo.fail, "UpdateHookDelivery")
	*f.clock = f.clock.Add(lease)
	f.ctl().drain(ctx)
	if d = f.delivery(t, id); d.Status != model.HookDeliveryPublished || d.Pending != nil {
		t.Errorf("recovered = %+v", d)
	}
	if events, actions := f.published(); len(events) != 4 || slices.Index(actions, entity.ActionHookPublish) < 0 {
		t.Errorf("published %d, telemetry %v", len(events), actions)
	}
}

func TestPublishSkipsAnEventGoneAndLogsAFailedPublish(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.pub.eventsErr = errBoom
	f.run.res = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push", Payload: "p"}}}
	id := f.receive(t)
	// The event is deleted after it was routed.
	d := f.delivery(t, id)
	pending, _ := json.Marshal([]model.HookPendingEvent{{HookEvent: "push", EventID: 777}, {HookEvent: "push", EventID: pushed}})
	d.Status, d.Pending, d.NextAttemptAt = model.HookDeliveryRouted, pending, start
	if err := f.repo.Repository.UpdateHookDelivery(ctx, d); err != nil {
		t.Fatal(err)
	}
	f.ctl().drain(ctx)
	events, _ := f.published()
	if d = f.delivery(t, id); d.Status != model.HookDeliveryPublished || len(events) != 1 || events[0].EventID != pushed {
		t.Errorf("delivery = %+v, published %+v", d, events)
	}
}

func TestDrainSurvivesTheRepository(t *testing.T) {
	ctx := context.Background()
	for _, method := range []string{"ListDueHookDeliveries", "ClaimHookDelivery", "GetHookDelivery"} {
		f := qSetup(t)
		id := f.receive(t)
		f.repo.fail[method] = errBoom
		f.ctl().drain(ctx)
		if f.run.calls != 0 {
			t.Errorf("%s: the hook ran", method)
		}
		if d := f.delivery(t, id); d.Status != model.HookDeliveryQueued && d.Status != model.HookDeliveryProcessing {
			t.Errorf("%s: delivery = %+v", method, d)
		}
	}
	// A failed write of the outcome is logged; the lease brings it back.
	f := qSetup(t)
	id := f.receive(t)
	f.repo.fail["UpdateHookDelivery"] = errBoom
	f.ctl().drain(ctx)
	if d := f.delivery(t, id); d.Status != model.HookDeliveryProcessing {
		t.Errorf("delivery = %+v", d)
	}
	// A routed delivery that cannot be read is left for its lease.
	f = qSetup(t)
	f.run.res = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push"}}}
	id = f.receive(t)
	calls := 0
	f.ctl().repo = &failNthDelivery{Repository: f.repo, n: 2, calls: &calls}
	f.ctl().drain(ctx)
	if d := f.delivery(t, id); d.Status != model.HookDeliveryRouted {
		t.Errorf("delivery = %+v", d)
	}
}

// failNthDelivery fails the nth GetHookDelivery.
type failNthDelivery struct {
	Repository
	n     int
	calls *int
}

func (f *failNthDelivery) GetHookDelivery(ctx context.Context, id int64) (model.HookDelivery, error) {
	*f.calls++
	if *f.calls == f.n {
		return model.HookDelivery{}, errBoom
	}
	return f.Repository.GetHookDelivery(ctx, id)
}

func TestDrainTakesEveryBatch(t *testing.T) {
	f := qSetup(t)
	ids := make([]int64, claimBatch+3)
	for i := range ids {
		ids[i] = f.receive(t)
	}
	f.ctl().drain(context.Background())
	for _, id := range ids {
		if d := f.delivery(t, id); d.Status != model.HookDeliveryIgnored {
			t.Fatalf("%d = %+v", id, d)
		}
	}
	// Claimed already: another worker's, left alone.
	f.ctl().work(context.Background(), model.HookDelivery{ID: ids[0], Status: model.HookDeliveryQueued})
	if f.run.calls != len(ids) {
		t.Errorf("ran %d times", f.run.calls)
	}
}

func TestStartWorksTheQueueAndSweeps(t *testing.T) {
	f := qSetup(t)
	old := f.receive(t)
	if err := f.repo.db.Model(&model.HookDelivery{}).Where("id = ?", old).
		Updates(map[string]any{"received_at": start.Add(-48 * time.Hour), "status": model.HookDeliveryPublished}).Error; err != nil {
		t.Fatal(err)
	}
	id := f.receive(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.c.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int64
		f.repo.db.Model(&model.HookDelivery{}).Where("id = ?", old).Count(&n)
		if d := f.delivery(t, id); d.Status == model.HookDeliveryIgnored && n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the queue was not worked, or not swept")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Woken by a new request.
	id = f.receive(t)
	for f.delivery(t, id).Status != model.HookDeliveryIgnored {
		if time.Now().After(deadline) {
			t.Fatal("a new request was not worked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	f.repo.fail["DeleteHookDeliveriesBefore"] = errBoom
	f.ctl().sweep(context.Background()) // logged
}

func TestListDeliveries(t *testing.T) {
	ctx := context.Background()
	f := qSetup(t)
	f.run.res = hookrunner.Result{Accept: true, Events: []hookrunner.EventOut{{Name: "push"}}}
	first := f.receive(t)
	f.ctl().drain(ctx)
	second := f.receive(t)

	ds, err := f.c.ListDeliveries(ctx, user, f.reg.ID, 0, 10)
	if err != nil || len(ds) != 2 || ds[0].ID != second || ds[0].Status != "queued" || ds[1].ID != first ||
		ds[1].Status != "published" || !slices.Equal(ds[1].EventNames, []string{"push"}) || ds[1].Replayable || ds[1].Attempts != 1 {
		t.Errorf("list = %+v, %v", ds, err)
	}
	if ds, err = f.c.ListDeliveries(ctx, user, f.reg.ID, second, 10); err != nil || len(ds) != 1 || ds[0].ID != first {
		t.Errorf("page = %+v, %v", ds, err)
	}
	if _, err := f.c.ListDeliveries(ctx, user+1, f.reg.ID, 0, 10); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account's = %v", err)
	}
	f.repo.fail["ListHookDeliveries"] = errBoom
	if _, err := f.c.ListDeliveries(ctx, user, f.reg.ID, 0, 10); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
}

func TestWindowsDropEndedOnesWhenMany(t *testing.T) {
	w := newWindows(1, time.Minute)
	for i := range verifiedCap {
		w.m[string(rune(i))] = &window{start: start.Add(-time.Hour), n: 1}
	}
	w.m["live"] = &window{start: start, n: 1}
	if !w.add("new", start) || w.add("new", start) || !w.full("live", start) || len(w.m) != 2 {
		t.Errorf("windows = %d", len(w.m))
	}
}
