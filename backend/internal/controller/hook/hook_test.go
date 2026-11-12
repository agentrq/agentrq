// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var errBoom = errors.New("boom")

type sqliteConn struct{ db *gorm.DB }

func (c sqliteConn) Conn(context.Context) *gorm.DB { return c.db }
func (c sqliteConn) Close(context.Context)         {}

// repo is the real repository, with a failure to inject per method.
type repo struct {
	base.Repository
	db   *gorm.DB
	fail map[string]error
	// notCreated has CreateHook report a name taken, as if a hook of the
	// name was added after the check.
	notCreated bool
}

func (r *repo) err(name string) error { return r.fail[name] }

func (r *repo) CreateHook(ctx context.Context, h model.Hook) (bool, error) {
	if err := r.err("CreateHook"); err != nil || r.notCreated {
		return false, err
	}
	return r.Repository.CreateHook(ctx, h)
}

func (r *repo) GetHook(ctx context.Context, id, userID int64) (model.Hook, error) {
	if err := r.err("GetHook"); err != nil {
		return model.Hook{}, err
	}
	return r.Repository.GetHook(ctx, id, userID)
}

func (r *repo) ListHooks(ctx context.Context, userID int64) ([]model.Hook, error) {
	if err := r.err("ListHooks"); err != nil {
		return nil, err
	}
	return r.Repository.ListHooks(ctx, userID)
}

func (r *repo) ReplaceHookModule(ctx context.Context, h model.Hook) error {
	if err := r.err("ReplaceHookModule"); err != nil {
		return err
	}
	return r.Repository.ReplaceHookModule(ctx, h)
}

func (r *repo) DeleteHook(ctx context.Context, id, userID int64) (bool, error) {
	if err := r.err("DeleteHook"); err != nil {
		return false, err
	}
	return r.Repository.DeleteHook(ctx, id, userID)
}

func (r *repo) ListHookRegistrationRoutes(ctx context.Context, hookID int64) ([]string, error) {
	if err := r.err("ListHookRegistrationRoutes"); err != nil {
		return nil, err
	}
	return r.Repository.ListHookRegistrationRoutes(ctx, hookID)
}

// blobs is storage in memory.
type blobs struct {
	mu        sync.Mutex
	m         map[string][]byte
	saveErr   error
	deleteErr error
}

func (b *blobs) Save(id, data string) error {
	if b.saveErr != nil {
		return b.saveErr
	}
	raw, _ := base64.StdEncoding.DecodeString(data)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[id] = raw
	return nil
}
func (b *blobs) Load(string) (string, error)      { return "", errBoom }
func (b *blobs) LoadRaw(string) ([]byte, error)   { return nil, errBoom }
func (b *blobs) keys() []string                   { return slices.Sorted(maps.Keys(b.m)) }
func (b *blobs) has(key string, wasm string) bool { return string(b.m[key]) == wasm }
func (b *blobs) Delete(id string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	delete(b.m, id)
	return nil
}

// inspector answers with the manifest registered for a module's bytes.
type inspector map[string]hookrunner.Manifest

func (in inspector) Inspect(_ context.Context, wasm []byte) (hookrunner.Manifest, [32]byte, error) {
	sum := sha256.Sum256(wasm)
	switch string(wasm) {
	case "invalid":
		return hookrunner.Manifest{}, sum, &hookrunner.InvalidModuleError{}
	case "broken":
		return hookrunner.Manifest{}, sum, errBoom
	}
	return in[string(wasm)], sum, nil
}

type ids struct{ n int64 }

func (i *ids) NextID() int64 { i.n++; return i.n }

func secret() hookrunner.Input {
	return hookrunner.Input{Name: "webhookSecret", Type: hookrunner.InputString, Secret: true, Required: true}
}

func manifest(name, version string, inputs []hookrunner.Input, events ...string) hookrunner.Manifest {
	m := hookrunner.Manifest{ABIVersion: 1, Name: name, Version: version, Description: name + " " + version, Inputs: inputs}
	for _, e := range events {
		m.Events = append(m.Events, hookrunner.Event{Name: e, Description: "on " + e})
	}
	return m
}

var modules = inspector{
	"github-1":  manifest("github", "1.0.0", []hookrunner.Input{secret(), {Name: "branches", Type: hookrunner.InputArray, Items: hookrunner.InputString}}, "ci.failed", "push"),
	"generic-1": manifest("generic", "1.0.0", []hookrunner.Input{secret()}, "request.received"),
	// An upgrade any registration keeps working with.
	"github-2":            manifest("github", "2.0.0", []hookrunner.Input{secret(), {Name: "branches", Type: hookrunner.InputArray, Items: hookrunner.InputString}, {Name: "label", Type: hookrunner.InputString}}, "ci.failed", "push", "release.published"),
	"github-no-push":      manifest("github", "3.0.0", []hookrunner.Input{secret()}, "ci.failed"),
	"github-needs-label":  manifest("github", "3.0.0", []hookrunner.Input{secret(), {Name: "label", Type: hookrunner.InputString, Required: true}}, "ci.failed", "push"),
	"github-branches-num": manifest("github", "3.0.0", []hookrunner.Input{secret(), {Name: "branches", Type: hookrunner.InputArray, Items: hookrunner.InputNumber}}, "ci.failed", "push"),
	"github-shown-secret": manifest("github", "3.0.0", []hookrunner.Input{{Name: "webhookSecret", Type: hookrunner.InputString, Required: true}, {Name: "token", Type: hookrunner.InputString, Secret: true, Required: true}}, "ci.failed", "push"),
}

type fixture struct {
	c     Controller
	repo  *repo
	blobs *blobs
}

func setup(t *testing.T) fixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Hook{}, &model.HookRegistration{}); err != nil {
		t.Fatal(err)
	}
	r := &repo{Repository: base.New(sqliteConn{db}), db: db, fail: map[string]error{}}
	b := &blobs{m: map[string][]byte{}}
	return fixture{c: New(Params{Repository: r, Storage: b, Runner: modules, IDGen: &ids{n: 100}}), repo: r, blobs: b}
}

func digest(wasm string) string {
	sum := sha256.Sum256([]byte(wasm))
	return hex.EncodeToString(sum[:])
}

func wantHookError(t *testing.T, err error, kind entity.HookErrorKind, text string) {
	t.Helper()
	var he *entity.HookError
	if !errors.As(err, &he) || he.Kind != kind || !strings.Contains(he.Message, text) {
		t.Fatalf("err = %v, want a hook error of kind %d naming %q", err, kind, text)
	}
}

var regID int64

func (f fixture) register(t *testing.T, hookID int64, routes string) {
	t.Helper()
	regID++
	reg := model.HookRegistration{ID: regID, UserID: 7, HookID: hookID, Name: "r", Token: "t", Routes: routes}
	if err := f.repo.db.Create(&reg).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCreateBuiltIn(t *testing.T) {
	f, ctx := setup(t), context.Background()
	h, err := f.c.CreateBuiltIn(ctx, []byte("github-1"))
	if err != nil {
		t.Fatal(err)
	}
	key := "u-0/hook-101/" + digest("github-1") + ".wasm"
	if h.ID != 101 || !h.BuiltIn || h.Name != "github" || h.Version != "1.0.0" || h.Description != "github 1.0.0" ||
		h.SHA256 != digest("github-1") || h.SizeBytes != 8 || h.ABIVersion != 1 || !h.Enabled || h.CreatedAt.IsZero() ||
		len(h.Inputs) != 2 || h.Inputs[0] != (entity.HookInput{Name: "webhookSecret", Type: "string", Secret: true, Required: true}) ||
		h.Inputs[1].Items != "string" || len(h.Events) != 2 || h.Events[1] != (entity.HookEvent{Name: "push", Description: "on push"}) {
		t.Errorf("hook = %+v", h)
	}
	if !f.blobs.has(key, "github-1") || len(f.blobs.m) != 1 {
		t.Errorf("blobs = %q", f.blobs.keys())
	}

	if _, err := f.c.CreateBuiltIn(ctx, []byte("generic-1")); err != nil {
		t.Fatal(err)
	}
	list, err := f.c.ListBuiltIns(ctx)
	if err != nil || len(list) != 2 || list[0].Name != "generic" || list[1].ID != 101 {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// A second of a name is refused before anything is stored.
	_, err = f.c.CreateBuiltIn(ctx, []byte("github-2"))
	wantHookError(t, err, entity.HookConflict, `already a hook "github"`)
	if len(f.blobs.m) != 2 {
		t.Errorf("blobs = %q", f.blobs.keys())
	}
}

func TestCreateBuiltInRefuses(t *testing.T) {
	ctx := context.Background()
	t.Run("an invalid module", func(t *testing.T) {
		_, err := setup(t).c.CreateBuiltIn(ctx, []byte("invalid"))
		wantHookError(t, err, entity.HookInvalid, "invalid hook")
	})
	t.Run("a failed inspection", func(t *testing.T) {
		if _, err := setup(t).c.CreateBuiltIn(ctx, []byte("broken")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a failed list", func(t *testing.T) {
		f := setup(t)
		f.repo.fail["ListHooks"] = errBoom
		if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
		if _, err := f.c.ListBuiltIns(ctx); !errors.Is(err, errBoom) {
			t.Errorf("ListBuiltIns err = %v", err)
		}
	})
	t.Run("a failed store", func(t *testing.T) {
		f := setup(t)
		f.blobs.saveErr = errBoom
		if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a name taken meanwhile, removing the module", func(t *testing.T) {
		f := setup(t)
		f.repo.notCreated = true
		_, err := f.c.CreateBuiltIn(ctx, []byte("github-1"))
		wantHookError(t, err, entity.HookConflict, "already")
		if len(f.blobs.m) != 0 {
			t.Errorf("blobs = %q", f.blobs.keys())
		}
	})
	t.Run("a failed insert, removing the module", func(t *testing.T) {
		f := setup(t)
		f.repo.fail["CreateHook"] = errBoom
		if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
		if len(f.blobs.m) != 0 {
			t.Errorf("blobs = %q", f.blobs.keys())
		}
	})
	t.Run("a failed read back", func(t *testing.T) {
		f := setup(t)
		f.repo.fail["GetHook"] = errBoom
		if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestUpgradeBuiltIn(t *testing.T) {
	f, ctx := setup(t), context.Background()
	if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); err != nil {
		t.Fatal(err)
	}
	f.register(t, 101, `{"ci.failed":"5","push":"6"}`)
	f.register(t, 101, `{"ci.failed":"5"}`)

	// The same module again changes nothing.
	same, err := f.c.UpgradeBuiltIn(ctx, 101, []byte("github-1"))
	if err != nil || same.Version != "1.0.0" {
		t.Fatalf("same module = %+v, %v", same, err)
	}

	h, err := f.c.UpgradeBuiltIn(ctx, 101, []byte("github-2"))
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != "2.0.0" || h.SHA256 != digest("github-2") || len(h.Events) != 3 || len(h.Inputs) != 3 || h.ID != 101 {
		t.Errorf("hook = %+v", h)
	}
	// The new module is stored beside, and the old one removed.
	if want := []string{"u-0/hook-101/" + digest("github-2") + ".wasm"}; !slices.Equal(f.blobs.keys(), want) {
		t.Errorf("blobs = %q, want %q", f.blobs.keys(), want)
	}
}

func TestUpgradeBuiltInRefusesToBreakARegistration(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		module, want string
	}{
		{"github-no-push", `no longer writes the event "push", routed by 1 registration;`},
		{"github-needs-label", `requires the input "label", missing from 2 registrations`},
		{"github-branches-num", `changes the type of the input "branches"`},
		{"github-shown-secret", `would show the secret input "webhookSecret"`},
	} {
		t.Run(tc.module, func(t *testing.T) {
			f := setup(t)
			if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); err != nil {
				t.Fatal(err)
			}
			f.register(t, 101, `{"ci.failed":"5","push":"6"}`)
			f.register(t, 101, `{"ci.failed":"5"}`)
			_, err := f.c.UpgradeBuiltIn(ctx, 101, []byte(tc.module))
			wantHookError(t, err, entity.HookConflict, tc.want)
			if !strings.HasSuffix(err.Error(), "; change or delete those registrations first") {
				t.Errorf("err = %v", err)
			}
			if !slices.Equal(f.blobs.keys(), []string{"u-0/hook-101/" + digest("github-1") + ".wasm"}) {
				t.Errorf("blobs = %q", f.blobs.keys())
			}
		})
	}

	// With no registration, anything of the same name goes.
	f := setup(t)
	if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); err != nil {
		t.Fatal(err)
	}
	if h, err := f.c.UpgradeBuiltIn(ctx, 101, []byte("github-shown-secret")); err != nil || h.Version != "3.0.0" {
		t.Errorf("unregistered upgrade = %+v, %v", h, err)
	}
}

func TestUpgradeBuiltInFails(t *testing.T) {
	ctx := context.Background()
	created := func(t *testing.T) fixture {
		f := setup(t)
		if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); err != nil {
			t.Fatal(err)
		}
		return f
	}
	t.Run("no such hook", func(t *testing.T) {
		if _, err := created(t).c.UpgradeBuiltIn(ctx, 999, []byte("github-2")); !errors.Is(err, base.ErrNotFound) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("an invalid module", func(t *testing.T) {
		_, err := created(t).c.UpgradeBuiltIn(ctx, 101, []byte("invalid"))
		wantHookError(t, err, entity.HookInvalid, "invalid hook")
	})
	t.Run("another hook", func(t *testing.T) {
		_, err := created(t).c.UpgradeBuiltIn(ctx, 101, []byte("generic-1"))
		wantHookError(t, err, entity.HookInvalid, `the hook "generic", not "github"`)
	})
	t.Run("a failed routes read", func(t *testing.T) {
		f := created(t)
		f.repo.fail["ListHookRegistrationRoutes"] = errBoom
		if _, err := f.c.UpgradeBuiltIn(ctx, 101, []byte("github-2")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a failed store", func(t *testing.T) {
		f := created(t)
		f.blobs.saveErr = errBoom
		if _, err := f.c.UpgradeBuiltIn(ctx, 101, []byte("github-2")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a failed update, removing the new module", func(t *testing.T) {
		f := created(t)
		f.repo.fail["ReplaceHookModule"] = errBoom
		if _, err := f.c.UpgradeBuiltIn(ctx, 101, []byte("github-2")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
		if !slices.Equal(f.blobs.keys(), []string{"u-0/hook-101/" + digest("github-1") + ".wasm"}) {
			t.Errorf("blobs = %q", f.blobs.keys())
		}
	})
	t.Run("a failed read back", func(t *testing.T) {
		f := created(t)
		var calls int
		f.repo.fail = nil
		c := f.c.(*controller)
		c.repo = &failNth{Repository: f.repo, n: 2, calls: &calls}
		if _, err := c.UpgradeBuiltIn(ctx, 101, []byte("github-2")); !errors.Is(err, errBoom) {
			t.Errorf("err = %v", err)
		}
	})
}

// failNth fails the nth GetHook.
type failNth struct {
	Repository
	n     int
	calls *int
}

func (f *failNth) GetHook(ctx context.Context, id, userID int64) (model.Hook, error) {
	*f.calls++
	if *f.calls == f.n {
		return model.Hook{}, errBoom
	}
	return f.Repository.GetHook(ctx, id, userID)
}

func TestSetBuiltInEnabled(t *testing.T) {
	f, ctx := setup(t), context.Background()
	if _, err := f.c.CreateBuiltIn(ctx, []byte("github-1")); err != nil {
		t.Fatal(err)
	}
	h, err := f.c.SetBuiltInEnabled(ctx, 101, false)
	if err != nil || h.Enabled {
		t.Fatalf("off = %+v, %v", h, err)
	}
	if h, err = f.c.SetBuiltInEnabled(ctx, 101, true); err != nil || !h.Enabled {
		t.Fatalf("on = %+v, %v", h, err)
	}
	if _, err := f.c.SetBuiltInEnabled(ctx, 999, true); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("no such hook: err = %v", err)
	}
	f.repo.fail["GetHook"] = errBoom
	if _, err := f.c.SetBuiltInEnabled(ctx, 101, true); !errors.Is(err, errBoom) {
		t.Errorf("failed read back: err = %v", err)
	}
}

func TestDeleteBuiltIn(t *testing.T) {
	f, ctx := setup(t), context.Background()
	for _, m := range []string{"github-1", "generic-1"} {
		if _, err := f.c.CreateBuiltIn(ctx, []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	f.register(t, 101, `{"push":"6"}`)

	wantHookError(t, f.c.DeleteBuiltIn(ctx, 101), entity.HookConflict, `"github" is registered`)
	if err := f.c.DeleteBuiltIn(ctx, 999); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("no such hook: err = %v", err)
	}
	f.repo.fail["DeleteHook"] = errBoom
	if err := f.c.DeleteBuiltIn(ctx, 102); !errors.Is(err, errBoom) {
		t.Errorf("failed delete: err = %v", err)
	}
	delete(f.repo.fail, "DeleteHook")
	// A module that cannot be removed is left behind, and only logged.
	f.blobs.deleteErr = errBoom
	if err := f.c.DeleteBuiltIn(ctx, 102); err != nil {
		t.Fatal(err)
	}
	f.blobs.deleteErr = nil
	if list, _ := f.c.ListBuiltIns(ctx); len(list) != 1 || list[0].Name != "github" {
		t.Errorf("list = %+v", list)
	}
}

func TestBlobKeyIsAStorageKey(t *testing.T) {
	if got := BlobKey(0, 7, "ab"); got != "u-0/hook-7/ab.wasm" {
		t.Errorf("BlobKey = %q", got)
	}
}
