// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
	"golang.org/x/crypto/bcrypt"
)

const (
	user     = 7
	key      = "0123456789abcdef0123456789abcdef"
	ciFailed = int64(500)
	pushed   = int64(501)
)

func (r *repo) CreateHookRegistration(ctx context.Context, reg model.HookRegistration) error {
	if err := r.err("CreateHookRegistration"); err != nil {
		return err
	}
	return r.Repository.CreateHookRegistration(ctx, reg)
}

func (r *repo) GetHookRegistration(ctx context.Context, id, userID int64) (model.HookRegistration, error) {
	if err := r.err("GetHookRegistration"); err != nil {
		return model.HookRegistration{}, err
	}
	return r.Repository.GetHookRegistration(ctx, id, userID)
}

func (r *repo) ListHookRegistrations(ctx context.Context, userID int64) ([]model.HookRegistration, error) {
	if err := r.err("ListHookRegistrations"); err != nil {
		return nil, err
	}
	return r.Repository.ListHookRegistrations(ctx, userID)
}

func (r *repo) UpdateHookRegistration(ctx context.Context, reg model.HookRegistration) error {
	if err := r.err("UpdateHookRegistration"); err != nil {
		return err
	}
	return r.Repository.UpdateHookRegistration(ctx, reg)
}

func (r *repo) SetHookRegistrationTokenHash(ctx context.Context, id, userID int64, hash string) error {
	if err := r.err("SetHookRegistrationTokenHash"); err != nil {
		return err
	}
	return r.Repository.SetHookRegistrationTokenHash(ctx, id, userID, hash)
}

// events is the account's events: user's ciFailed and pushed, a trigger on
// pushed that allows every command.
type events struct {
	getErr, triggersErr error
}

func (e *events) GetEvent(_ context.Context, id, userID int64) (model.Event, error) {
	if e.getErr != nil {
		return model.Event{}, e.getErr
	}
	if userID != user || (id != ciFailed && id != pushed) {
		return model.Event{}, base.ErrNotFound
	}
	return model.Event{ID: id, UserID: userID}, nil
}

func (e *events) ListEventTriggersByEvent(_ context.Context, id, _ int64) ([]model.EventTrigger, error) {
	if e.triggersErr != nil {
		return nil, e.triggersErr
	}
	return []model.EventTrigger{{AllowAllCommands: false}, {AllowAllCommands: id == pushed}}, nil
}

type publisher struct{ got []pubsub.PublishRequest }

func (p *publisher) Publish(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
	p.got = append(p.got, req)
	return nil, nil
}

type regFixture struct {
	fixture
	events *events
	pub    *publisher
	// github is a built-in registrations use; generic another, off.
	github, generic, own int64
}

func regSetup(t *testing.T, allowUserHooks bool) regFixture {
	t.Helper()
	f := setup(t)
	if err := f.repo.db.AutoMigrate(&model.HookDelivery{}, &model.HookDeliveryKey{}); err != nil {
		t.Fatal(err)
	}
	ev, pub := &events{}, &publisher{}
	f.c = New(Params{Repository: f.repo, Storage: f.blobs, Runner: modules, IDGen: &ids{n: 100}, Events: ev, PubSub: pub,
		AllowUserHooks: allowUserHooks, TokenKey: key, BaseURL: "https://rq.example"})
	ctx := context.Background()
	gh, err := f.c.CreateBuiltIn(ctx, []byte("github-1"))
	if err != nil {
		t.Fatal(err)
	}
	gen, err := f.c.CreateBuiltIn(ctx, []byte("generic-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.SetBuiltInEnabled(ctx, gen.ID, false); err != nil {
		t.Fatal(err)
	}
	// The account's own hook, as user hooks will add it.
	own := model.Hook{ID: 900, UserID: user, Name: "mine", Version: "1.0.0", SHA256: "s", Manifest: `{"abiVersion":1,"name":"mine","inputs":[{"name":"webhookSecret","type":"string","secret":true,"required":true}],"events":[{"name":"x"}]}`, Enabled: true}
	if err := f.repo.db.Create(&own).Error; err != nil {
		t.Fatal(err)
	}
	return regFixture{fixture: f, events: ev, pub: pub, github: gh.ID, generic: gen.ID, own: own.ID}
}

func ptr[T any](v T) *T { return &v }

func (f regFixture) create(t *testing.T) entity.HookRegistration {
	t.Helper()
	reg, err := f.c.CreateRegistration(context.Background(), user, entity.HookRegistrationRequest{
		HookID: f.github, Name: ptr(" CI "),
		Inputs: map[string]any{"webhookSecret": "s3cret", "branches": []any{"main"}},
		Routes: map[string]int64{"ci.failed": ciFailed, "push": pushed},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestListAvailable(t *testing.T) {
	ctx := context.Background()
	names := func(hs []entity.Hook) (out []string) {
		for _, h := range hs {
			out = append(out, h.Name)
		}
		return out
	}

	f := regSetup(t, false)
	hooks, err := f.c.ListAvailable(ctx, user)
	if err != nil || !slices.Equal(names(hooks), []string{"github"}) {
		t.Errorf("without user hooks = %v, %v", names(hooks), err)
	}

	f = regSetup(t, true)
	if hooks, err = f.c.ListAvailable(ctx, user); err != nil || !slices.Equal(names(hooks), []string{"github", "mine"}) {
		t.Errorf("with user hooks = %v, %v", names(hooks), err)
	}
	// The built-ins' owner has no hooks of its own to add twice.
	if hooks, err = f.c.ListAvailable(ctx, builtIn); err != nil || !slices.Equal(names(hooks), []string{"github"}) {
		t.Errorf("for the built-ins' owner = %v, %v", names(hooks), err)
	}

	f.repo.fail["ListHooks"] = errBoom
	if _, err := f.c.ListAvailable(ctx, user); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
	// The second list, of the account's own, fails.
	f.repo.fail = map[string]error{}
	calls := 0
	f.c.(*controller).repo = &failNthList{Repository: f.repo, n: 2, calls: &calls}
	if _, err := f.c.ListAvailable(ctx, user); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
}

// failNthList fails the nth ListHooks.
type failNthList struct {
	Repository
	n     int
	calls *int
}

func (f *failNthList) ListHooks(ctx context.Context, userID int64) ([]model.Hook, error) {
	*f.calls++
	if *f.calls == f.n {
		return nil, errBoom
	}
	return f.Repository.ListHooks(ctx, userID)
}

func TestCreateRegistration(t *testing.T) {
	f := regSetup(t, false)
	reg := f.create(t)

	if reg.Name != "CI" || !reg.Enabled || reg.HookName != "github" || !reg.HookAvailable || reg.HookID != f.github {
		t.Errorf("registration = %+v", reg)
	}
	if want := "https://rq.example/api/v1/hooks/in/" + monoflake.ID(reg.ID).String(); reg.URL != want {
		t.Errorf("url = %q, want %q", reg.URL, want)
	}
	prefix := reg.URL + "?agentrq_hook_token="
	token := strings.TrimPrefix(reg.URLWithToken, prefix)
	if !strings.HasPrefix(reg.URLWithToken, prefix) || len(token) != tokenLen || strings.Trim(token, tokenChars) != "" {
		t.Errorf("url with token = %q", reg.URLWithToken)
	}
	if !slices.Equal(reg.SecretsSet, []string{"webhookSecret"}) || len(reg.Inputs) != 1 || reg.Inputs["branches"] == nil {
		t.Errorf("inputs = %v, secrets set %v", reg.Inputs, reg.SecretsSet)
	}
	if reg.Routes["ci.failed"] != ciFailed || reg.Routes["push"] != pushed || !slices.Equal(reg.AllowAllCommandsEvents, []string{"push"}) {
		t.Errorf("routes = %v, allow all %v", reg.Routes, reg.AllowAllCommandsEvents)
	}

	// Stored: the token only as its hash, the secret only sealed.
	var m model.HookRegistration
	if err := f.repo.db.First(&m, reg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(m.TokenHash), []byte(token)) != nil || strings.Contains(m.Inputs, "s3cret") || m.UserID != user {
		t.Errorf("stored = %+v", m)
	}

	if len(f.pub.got) != 1 {
		t.Fatalf("published %d", len(f.pub.got))
	}
	ev := f.pub.got[0].Event.(entity.CRUDEvent)
	if f.pub.got[0].PubSubID != entity.PubSubTopicCRUD || ev.Action != entity.ActionHookRegister || ev.ResourceType != entity.ResourceHookRegistration ||
		ev.ResourceID != reg.ID || ev.UserID != user || ev.Origin != entity.OriginAPI || ev.Actor != entity.ActorHuman {
		t.Errorf("telemetry = %+v", ev)
	}

	// Made off, from an agent; nothing routed.
	ctx := entity.WithOrigin(context.Background(), entity.OriginMCP)
	reg, err := f.c.CreateRegistration(ctx, user, entity.HookRegistrationRequest{HookID: f.github, Name: ptr("off"), Enabled: ptr(false),
		Inputs: map[string]any{"webhookSecret": "s"}})
	if err != nil || reg.Enabled || len(reg.Routes) != 0 || reg.AllowAllCommandsEvents != nil {
		t.Errorf("off = %+v, %v", reg, err)
	}
	if ev := f.pub.got[1].Event.(entity.CRUDEvent); ev.Origin != entity.OriginMCP {
		t.Errorf("origin = %v", ev.Origin)
	}

	// No telemetry is sent without a publisher.
	f.c.(*controller).pubsub = nil
	f.create(t)
	if len(f.pub.got) != 2 {
		t.Errorf("published %d", len(f.pub.got))
	}
}

func TestCreateRegistrationOfAnOwnHook(t *testing.T) {
	ctx := context.Background()
	req := entity.HookRegistrationRequest{Name: ptr("mine"), Inputs: map[string]any{"webhookSecret": "s"}}

	f := regSetup(t, true)
	req.HookID = f.own
	if reg, err := f.c.CreateRegistration(ctx, user, req); err != nil || reg.HookName != "mine" {
		t.Errorf("own hook = %+v, %v", reg, err)
	}
	// Another account's hook is not one this account may use.
	if _, err := f.c.CreateRegistration(ctx, user+1, req); err == nil {
		t.Error("another account's hook was registered")
	}

	// Nor its own, once user hooks are not allowed.
	f = regSetup(t, false)
	req.HookID = f.own
	_, err := f.c.CreateRegistration(ctx, user, req)
	wantHookError(t, err, entity.HookInvalid, "there is no hook")
}

func TestCreateRegistrationRefuses(t *testing.T) {
	long := strings.Repeat("x", maxInputString+1)
	for name, tc := range map[string]struct {
		req  func(f regFixture) entity.HookRegistrationRequest
		text string
	}{
		"a hook turned off": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.generic, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s"}}
		}, "there is no hook"},
		"no hook": {func(regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: 12345, Name: ptr("n")}
		}, "there is no hook"},
		"no name": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("  "), Inputs: map[string]any{"webhookSecret": "s"}}
		}, "a name of 1 to 255"},
		"a name left out": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Inputs: map[string]any{"webhookSecret": "s"}}
		}, "a name of 1 to 255"},
		"a long name": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr(strings.Repeat("é", 256)), Inputs: map[string]any{"webhookSecret": "s"}}
		}, "a name of 1 to 255"},
		"no secret": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n")}
		}, `the input "webhookSecret" is required`},
		"an empty secret": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": ""}}
		}, `the input "webhookSecret" is required`},
		"an input it has not": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s", "x": 1.0}}
		}, `no input "x"`},
		"a long string": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": long}}
		}, `"webhookSecret" must be a string`},
		"a number for a string": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": 1.0}}
		}, `"webhookSecret" must be a string`},
		"not a list": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s", "branches": "main"}}
		}, `"branches" must be a list of at most 64`},
		"a long list": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s", "branches": make([]any, maxInputItems+1)}}
		}, `"branches" must be a list of at most 64`},
		"a list of another type": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s", "branches": []any{"main", true}}}
		}, `"branches" must be a string`},
		"an event it writes not": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s"}, Routes: map[string]int64{"deploy": ciFailed}}
		}, `writes no event "deploy"`},
		"another account's event": {func(f regFixture) entity.HookRegistrationRequest {
			return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s"}, Routes: map[string]int64{"push": 999}}
		}, `routed from "push" is not one of this account's`},
	} {
		t.Run(name, func(t *testing.T) {
			f := regSetup(t, false)
			_, err := f.c.CreateRegistration(context.Background(), user, tc.req(f))
			wantHookError(t, err, entity.HookInvalid, tc.text)
			if len(f.pub.got) != 0 {
				t.Error("a refused registration sent telemetry")
			}
		})
	}
}

func TestCheckValue(t *testing.T) {
	for _, tc := range []struct {
		typ string
		v   any
		ok  bool
	}{
		{hookrunner.InputNumber, 1.5, true},
		{hookrunner.InputNumber, math.Inf(1), false},
		{hookrunner.InputNumber, math.NaN(), false},
		{hookrunner.InputNumber, "1", false},
		{hookrunner.InputBoolean, true, true},
		{hookrunner.InputBoolean, "true", false},
		{hookrunner.InputString, "s", true},
	} {
		if err := checkValue(hookrunner.Input{Name: "v", Type: tc.typ}, tc.v); (err == nil) != tc.ok {
			t.Errorf("%s %v: %v", tc.typ, tc.v, err)
		}
	}
}

func TestCreateRegistrationFails(t *testing.T) {
	ctx := context.Background()
	req := func(f regFixture) entity.HookRegistrationRequest {
		return entity.HookRegistrationRequest{HookID: f.github, Name: ptr("n"), Inputs: map[string]any{"webhookSecret": "s"}, Routes: map[string]int64{"push": pushed}}
	}
	for name, fail := range map[string]func(f regFixture){
		"reading the hook":     func(f regFixture) { f.repo.fail["GetHook"] = errBoom },
		"reading the event":    func(f regFixture) { f.events.getErr = errBoom },
		"reading its triggers": func(f regFixture) { f.events.triggersErr = errBoom },
		"storing it":           func(f regFixture) { f.repo.fail["CreateHookRegistration"] = errBoom },
	} {
		t.Run(name, func(t *testing.T) {
			f := regSetup(t, false)
			fail(f)
			if _, err := f.c.CreateRegistration(ctx, user, req(f)); !errors.Is(err, errBoom) {
				t.Errorf("err = %v", err)
			}
		})
	}
	t.Run("sealing the inputs", func(t *testing.T) {
		f := regSetup(t, false)
		f.c.(*controller).tokenKey = "short"
		if _, err := f.c.CreateRegistration(ctx, user, req(f)); err == nil || !strings.Contains(err.Error(), "seal inputs") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestGetAndListRegistrations(t *testing.T) {
	ctx := context.Background()
	f := regSetup(t, false)
	first := f.create(t)
	second := f.create(t)

	got, err := f.c.GetRegistration(ctx, user, first.ID)
	if err != nil || got.ID != first.ID || got.URLWithToken != "" || !got.HookAvailable || got.Inputs["branches"] == nil {
		t.Errorf("get = %+v, %v", got, err)
	}
	if _, err := f.c.GetRegistration(ctx, user+1, first.ID); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account's = %v", err)
	}

	regs, err := f.c.ListRegistrations(ctx, user)
	if err != nil || len(regs) != 2 || regs[0].ID != second.ID || regs[1].ID != first.ID || regs[0].URLWithToken != "" {
		t.Errorf("list = %+v, %v", regs, err)
	}

	// Its hook turned off, or gone: still listed, as not available.
	if _, err := f.c.SetBuiltInEnabled(ctx, f.github, false); err != nil {
		t.Fatal(err)
	}
	if got, err = f.c.GetRegistration(ctx, user, first.ID); err != nil || got.HookAvailable || got.HookName != "github" {
		t.Errorf("hook off = %+v, %v", got, err)
	}
	if err := f.repo.db.Delete(&model.Hook{}, f.github).Error; err != nil {
		t.Fatal(err)
	}
	if got, err = f.c.GetRegistration(ctx, user, first.ID); err != nil || got.HookAvailable || got.HookName != "" {
		t.Errorf("hook gone = %+v, %v", got, err)
	}
	// A hook that is gone hides no input as secret; it shows them all.
	if regs, err = f.c.ListRegistrations(ctx, user); err != nil || len(regs) != 2 || regs[0].HookAvailable || regs[0].Inputs["webhookSecret"] != "s3cret" {
		t.Errorf("list, hook gone = %+v, %v", regs, err)
	}
}

func TestGetAndListRegistrationsFail(t *testing.T) {
	ctx := context.Background()
	for name, fail := range map[string]func(f regFixture){
		"reading the registration": func(f regFixture) {
			f.repo.fail["GetHookRegistration"] = errBoom
			f.repo.fail["ListHookRegistrations"] = errBoom
		},
		"reading the hook":      func(f regFixture) { f.repo.fail["GetHook"] = errBoom },
		"reading the triggers":  func(f regFixture) { f.events.triggersErr = errBoom },
		"opening the inputs":    func(f regFixture) { f.c.(*controller).tokenKey = strings.Repeat("k", 32) },
		"opening sealed inputs": func(f regFixture) { f.repo.db.Model(&model.HookRegistration{}).Where("1 = 1").Update("inputs", "x") },
	} {
		t.Run(name, func(t *testing.T) {
			f := regSetup(t, false)
			reg := f.create(t)
			fail(f)
			if _, err := f.c.GetRegistration(ctx, user, reg.ID); err == nil {
				t.Error("get did not fail")
			}
			if _, err := f.c.ListRegistrations(ctx, user); err == nil {
				t.Error("list did not fail")
			}
		})
	}
}

func TestUpdateRegistration(t *testing.T) {
	ctx := context.Background()
	f := regSetup(t, false)
	reg := f.create(t)

	// Only the name: the inputs, secrets and routes are kept.
	got, err := f.c.UpdateRegistration(ctx, user, reg.ID, entity.HookRegistrationRequest{Name: ptr("renamed")})
	if err != nil || got.Name != "renamed" || !got.Enabled || got.URLWithToken != "" ||
		!slices.Equal(got.SecretsSet, []string{"webhookSecret"}) || got.Inputs["branches"] == nil || len(got.Routes) != 2 {
		t.Errorf("rename = %+v, %v", got, err)
	}

	// New inputs without the secret keep it; the routes given replace them.
	got, err = f.c.UpdateRegistration(ctx, user, reg.ID, entity.HookRegistrationRequest{Enabled: ptr(false),
		Inputs: map[string]any{"branches": []any{"dev"}}, Routes: map[string]int64{"push": pushed}})
	if err != nil || got.Enabled || !slices.Equal(got.SecretsSet, []string{"webhookSecret"}) ||
		!slices.Equal(got.Inputs["branches"].([]any), []any{"dev"}) || len(got.Routes) != 1 {
		t.Errorf("update = %+v, %v", got, err)
	}
	stored, _ := f.c.(*controller).inputsOf(mustGet(t, f, reg.ID))
	if stored["webhookSecret"] != "s3cret" {
		t.Errorf("stored inputs = %v", stored)
	}

	// A list emptied drops it; an empty secret is no secret, so is refused.
	if got, err = f.c.UpdateRegistration(ctx, user, reg.ID, entity.HookRegistrationRequest{Inputs: map[string]any{"branches": nil}}); err != nil || got.Inputs["branches"] != nil {
		t.Errorf("drop = %+v, %v", got, err)
	}
	_, err = f.c.UpdateRegistration(ctx, user, reg.ID, entity.HookRegistrationRequest{Inputs: map[string]any{"webhookSecret": ""}})
	wantHookError(t, err, entity.HookInvalid, `"webhookSecret" is required`)
	if len(f.pub.got) != 1 {
		t.Errorf("an update sent telemetry: %d", len(f.pub.got))
	}
}

func mustGet(t *testing.T, f regFixture, id int64) model.HookRegistration {
	t.Helper()
	reg, err := f.repo.Repository.GetHookRegistration(context.Background(), id, user)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestUpdateRegistrationRefuses(t *testing.T) {
	ctx := context.Background()
	f := regSetup(t, false)
	reg := f.create(t)
	if _, err := f.c.UpdateRegistration(ctx, user+1, reg.ID, entity.HookRegistrationRequest{Name: ptr("x")}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account's = %v", err)
	}
	if _, err := f.c.SetBuiltInEnabled(ctx, f.github, false); err != nil {
		t.Fatal(err)
	}
	_, err := f.c.UpdateRegistration(ctx, user, reg.ID, entity.HookRegistrationRequest{Name: ptr("x")})
	wantHookError(t, err, entity.HookInvalid, "delete this registration instead")
}

func TestUpdateRegistrationFails(t *testing.T) {
	ctx := context.Background()
	for name, fail := range map[string]func(f regFixture){
		"reading the hook":   func(f regFixture) { f.repo.fail["GetHook"] = errBoom },
		"opening the inputs": func(f regFixture) { f.c.(*controller).tokenKey = strings.Repeat("k", 32) },
		"reading the event":  func(f regFixture) { f.events.getErr = errBoom },
		"storing it":         func(f regFixture) { f.repo.fail["UpdateHookRegistration"] = errBoom },
	} {
		t.Run(name, func(t *testing.T) {
			f := regSetup(t, false)
			reg := f.create(t)
			fail(f)
			if _, err := f.c.UpdateRegistration(ctx, user, reg.ID, entity.HookRegistrationRequest{Name: ptr("x")}); err == nil || errors.As(err, new(*entity.HookError)) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestRotateToken(t *testing.T) {
	ctx := context.Background()
	f := regSetup(t, false)
	reg := f.create(t)
	before := mustGet(t, f, reg.ID).TokenHash

	got, err := f.c.RotateToken(ctx, user, reg.ID)
	if err != nil || got.URLWithToken == "" || got.URLWithToken == reg.URLWithToken || got.Name != "CI" {
		t.Fatalf("rotate = %+v, %v", got, err)
	}
	after := mustGet(t, f, reg.ID).TokenHash
	token := got.URLWithToken[strings.LastIndex(got.URLWithToken, "=")+1:]
	if after == before || bcrypt.CompareHashAndPassword([]byte(after), []byte(token)) != nil {
		t.Error("the new token is not the one stored")
	}

	// A hook that is gone does not stop a rotation.
	if err := f.repo.db.Delete(&model.Hook{}, f.github).Error; err != nil {
		t.Fatal(err)
	}
	if got, err = f.c.RotateToken(ctx, user, reg.ID); err != nil || got.HookAvailable {
		t.Errorf("hook gone = %+v, %v", got, err)
	}

	if _, err := f.c.RotateToken(ctx, user+1, reg.ID); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account's = %v", err)
	}
	for _, method := range []string{"SetHookRegistrationTokenHash", "GetHook"} {
		f.repo.fail = map[string]error{method: errBoom}
		if _, err := f.c.RotateToken(ctx, user, reg.ID); !errors.Is(err, errBoom) {
			t.Errorf("%s: err = %v", method, err)
		}
	}
}

func TestDeleteRegistration(t *testing.T) {
	ctx := context.Background()
	f := regSetup(t, false)
	reg := f.create(t)
	if err := f.c.DeleteRegistration(ctx, user+1, reg.ID); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("another account's = %v", err)
	}
	if err := f.c.DeleteRegistration(ctx, user, reg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.GetRegistration(ctx, user, reg.ID); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("after delete = %v", err)
	}
}

func TestNewTokenIsUniform(t *testing.T) {
	seen := map[byte]bool{}
	for range 20 {
		token, hash := newToken()
		if len(token) != tokenLen || bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) != nil {
			t.Fatalf("token %q, hash %q", token, hash)
		}
		for i := range len(token) {
			seen[token[i]] = true
		}
	}
	for c := range seen {
		if !strings.ContainsRune(tokenChars, rune(c)) {
			t.Errorf("%q is not base36", c)
		}
	}
}
