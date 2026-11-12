// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	zlog "github.com/rs/zerolog/log"
)

// builtIn is the user id of a built-in hook, the root user's for every
// account.
const builtIn = 0

// Controller manages hooks.
type Controller interface {
	// ListBuiltIns returns the built-in hooks, by name.
	ListBuiltIns(ctx context.Context) ([]entity.Hook, error)
	// CreateBuiltIn adds a built-in hook from its module.
	CreateBuiltIn(ctx context.Context, wasm []byte) (entity.Hook, error)
	// UpgradeBuiltIn replaces a built-in hook's module with a new version of
	// the same hook, unless that would break a registration of it.
	UpgradeBuiltIn(ctx context.Context, id int64, wasm []byte) (entity.Hook, error)
	// SetBuiltInEnabled turns a built-in hook on or off for every account.
	SetBuiltInEnabled(ctx context.Context, id int64, enabled bool) (entity.Hook, error)
	// DeleteBuiltIn removes a built-in hook that nothing registers.
	DeleteBuiltIn(ctx context.Context, id int64) error
}

// Repository is what the controller reads and writes.
type Repository interface {
	CreateHook(ctx context.Context, h model.Hook) (bool, error)
	GetHook(ctx context.Context, id, userID int64) (model.Hook, error)
	ListHooks(ctx context.Context, userID int64) ([]model.Hook, error)
	ReplaceHookModule(ctx context.Context, h model.Hook) error
	SetHookEnabled(ctx context.Context, id, userID int64, enabled bool) error
	DeleteHook(ctx context.Context, id, userID int64) (bool, error)
	ListHookRegistrationRoutes(ctx context.Context, hookID int64) ([]string, error)
}

// Inspector reads a module's manifest; hookrunner.Service is one.
type Inspector interface {
	Inspect(ctx context.Context, wasm []byte) (hookrunner.Manifest, [32]byte, error)
}

// Params are a controller's dependencies.
type Params struct {
	Repository Repository
	// Storage keeps the modules. It must not be served: a hook may be the
	// account's own.
	Storage storage.Service
	Runner  Inspector
	IDGen   idgen.Service
}

type controller struct {
	repo   Repository
	blobs  storage.Service
	runner Inspector
	ids    idgen.Service
}

// New returns a hooks controller.
func New(p Params) Controller {
	return &controller{repo: p.Repository, blobs: p.Storage, runner: p.Runner, ids: p.IDGen}
}

func (c *controller) ListBuiltIns(ctx context.Context) ([]entity.Hook, error) {
	hooks, err := c.repo.ListHooks(ctx, builtIn)
	if err != nil {
		return nil, err
	}
	out := make([]entity.Hook, len(hooks))
	for i, h := range hooks {
		out[i] = toEntity(h)
	}
	return out, nil
}

func (c *controller) CreateBuiltIn(ctx context.Context, wasm []byte) (entity.Hook, error) {
	return c.create(ctx, builtIn, wasm)
}

func (c *controller) UpgradeBuiltIn(ctx context.Context, id int64, wasm []byte) (entity.Hook, error) {
	return c.upgrade(ctx, builtIn, id, wasm)
}

func (c *controller) SetBuiltInEnabled(ctx context.Context, id int64, enabled bool) (entity.Hook, error) {
	if err := c.repo.SetHookEnabled(ctx, id, builtIn, enabled); err != nil {
		return entity.Hook{}, err
	}
	h, err := c.repo.GetHook(ctx, id, builtIn)
	if err != nil {
		return entity.Hook{}, err
	}
	return toEntity(h), nil
}

func (c *controller) DeleteBuiltIn(ctx context.Context, id int64) error {
	return c.delete(ctx, builtIn, id)
}

// create stores the module first and the row after: a row never names a
// module that is not there, and a module left by a failed insert is removed.
func (c *controller) create(ctx context.Context, userID int64, wasm []byte) (entity.Hook, error) {
	m, sum, err := c.inspect(ctx, wasm)
	if err != nil {
		return entity.Hook{}, err
	}
	hooks, err := c.repo.ListHooks(ctx, userID)
	if err != nil {
		return entity.Hook{}, err
	}
	for _, h := range hooks {
		if h.Name == m.Name {
			return entity.Hook{}, nameTaken(m.Name)
		}
	}
	h := model.Hook{ID: c.ids.NextID(), UserID: userID, Enabled: true}
	setModule(&h, m, sum, len(wasm))
	if err := c.save(h.BlobKey, wasm); err != nil {
		return entity.Hook{}, err
	}
	created, err := c.repo.CreateHook(ctx, h)
	if err == nil && !created {
		// Another request added one of this name since the check above.
		err = nameTaken(m.Name)
	}
	if err != nil {
		c.remove(h.BlobKey)
		return entity.Hook{}, err
	}
	h, err = c.repo.GetHook(ctx, h.ID, userID)
	if err != nil {
		return entity.Hook{}, err
	}
	return toEntity(h), nil
}

// upgrade swaps in a new module of the same hook: stored first, then the row
// pointed at it, then the old one removed. A delivery that read the row
// before the swap fails to load the old module and is retried with the new.
func (c *controller) upgrade(ctx context.Context, userID, id int64, wasm []byte) (entity.Hook, error) {
	h, err := c.repo.GetHook(ctx, id, userID)
	if err != nil {
		return entity.Hook{}, err
	}
	m, sum, err := c.inspect(ctx, wasm)
	if err != nil {
		return entity.Hook{}, err
	}
	if m.Name != h.Name {
		return entity.Hook{}, &entity.HookError{Kind: entity.HookInvalid,
			Message: fmt.Sprintf("this module is the hook %q, not %q; add it as a new hook instead", m.Name, h.Name)}
	}
	if hex.EncodeToString(sum[:]) == h.SHA256 {
		return toEntity(h), nil
	}
	if err := c.compatible(ctx, h, m); err != nil {
		return entity.Hook{}, err
	}
	oldKey := h.BlobKey
	setModule(&h, m, sum, len(wasm))
	if err := c.save(h.BlobKey, wasm); err != nil {
		return entity.Hook{}, err
	}
	if err := c.repo.ReplaceHookModule(ctx, h); err != nil {
		c.remove(h.BlobKey)
		return entity.Hook{}, err
	}
	c.remove(oldKey)
	h, err = c.repo.GetHook(ctx, id, userID)
	if err != nil {
		return entity.Hook{}, err
	}
	return toEntity(h), nil
}

// compatible refuses an upgrade that would break a registration of the hook:
// one dropping an event a registration routes, or asking a value of an input
// that the registrations do not have, or have of another kind.
func (c *controller) compatible(ctx context.Context, h model.Hook, m hookrunner.Manifest) error {
	routes, err := c.repo.ListHookRegistrationRoutes(ctx, h.ID)
	if err != nil || len(routes) == 0 {
		return err
	}
	refuse := func(format string, a ...any) error {
		return &entity.HookError{Kind: entity.HookConflict,
			Message: fmt.Sprintf(format, a...) + "; change or delete those registrations first"}
	}
	routed := map[string]int{}
	for _, r := range routes {
		var byEvent map[string]json.RawMessage
		// Written by this server as a JSON object.
		_ = json.Unmarshal([]byte(r), &byEvent)
		for name := range byEvent {
			routed[name]++
		}
	}
	for name, n := range routed {
		if !m.Declares(name) {
			return refuse("version %s no longer writes the event %q, routed by %s", m.Version, name, plural(n, "registration"))
		}
	}
	old := manifestOf(h)
	before := make(map[string]hookrunner.Input, len(old.Inputs))
	for _, in := range old.Inputs {
		before[in.Name] = in
	}
	n := len(routes)
	for _, in := range m.Inputs {
		was, ok := before[in.Name]
		switch {
		case in.Required && (!ok || !was.Required):
			return refuse("version %s requires the input %q, missing from %s", m.Version, in.Name, plural(n, "registration"))
		case ok && (was.Type != in.Type || was.Items != in.Items):
			return refuse("version %s changes the type of the input %q, set in %s", m.Version, in.Name, plural(n, "registration"))
		case ok && was.Secret && !in.Secret:
			return refuse("version %s would show the secret input %q, set in %s", m.Version, in.Name, plural(n, "registration"))
		}
	}
	return nil
}

// delete removes the row, then the module: a module is never removed from
// under a row that still names it.
func (c *controller) delete(ctx context.Context, userID, id int64) error {
	h, err := c.repo.GetHook(ctx, id, userID)
	if err != nil {
		return err
	}
	deleted, err := c.repo.DeleteHook(ctx, id, userID)
	if err != nil {
		return err
	}
	if !deleted {
		return &entity.HookError{Kind: entity.HookConflict,
			Message: fmt.Sprintf("the hook %q is registered; delete its registrations first", h.Name)}
	}
	c.remove(h.BlobKey)
	return nil
}

func (c *controller) inspect(ctx context.Context, wasm []byte) (hookrunner.Manifest, [32]byte, error) {
	m, sum, err := c.runner.Inspect(ctx, wasm)
	var invalid *hookrunner.InvalidModuleError
	if errors.As(err, &invalid) {
		return m, sum, &entity.HookError{Kind: entity.HookInvalid, Message: invalid.Error()}
	}
	return m, sum, err
}

func (c *controller) save(key string, wasm []byte) error {
	if err := c.blobs.Save(key, base64.StdEncoding.EncodeToString(wasm)); err != nil {
		return fmt.Errorf("hook: store module: %w", err)
	}
	return nil
}

// remove deletes a module nothing names any more. A failure leaves an
// orphan in storage, never a hook without its module, so it is only logged.
func (c *controller) remove(key string) {
	if err := c.blobs.Delete(key); err != nil {
		zlog.Warn().Err(err).Str("key", key).Msg("hook: could not delete a module")
	}
}

// BlobKey is where a hook's module is kept: by account, hook and digest, so
// an upgrade writes beside the module it replaces.
func BlobKey(userID, hookID int64, sha256Hex string) string {
	return fmt.Sprintf("u-%d/hook-%d/%s.wasm", userID, hookID, sha256Hex)
}

func setModule(h *model.Hook, m hookrunner.Manifest, sum [32]byte, size int) {
	b, _ := json.Marshal(m) // a Manifest is plain data
	h.Name = m.Name
	h.Version = m.Version
	h.SHA256 = hex.EncodeToString(sum[:])
	h.BlobKey = BlobKey(h.UserID, h.ID, h.SHA256)
	h.SizeBytes = int64(size)
	h.ABIVersion = int16(m.ABIVersion)
	h.Manifest = string(b)
}

func manifestOf(h model.Hook) hookrunner.Manifest {
	var m hookrunner.Manifest
	// Written by setModule from a manifest the runner checked.
	_ = json.Unmarshal([]byte(h.Manifest), &m)
	return m
}

func toEntity(h model.Hook) entity.Hook {
	m := manifestOf(h)
	e := entity.Hook{
		ID: h.ID, BuiltIn: h.UserID == builtIn, Name: h.Name, Version: h.Version, Description: m.Description,
		SHA256: h.SHA256, SizeBytes: h.SizeBytes, ABIVersion: int(h.ABIVersion),
		Inputs: make([]entity.HookInput, len(m.Inputs)), Events: make([]entity.HookEvent, len(m.Events)),
		Enabled: h.Enabled, CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
	}
	for i, in := range m.Inputs {
		e.Inputs[i] = entity.HookInput{Name: in.Name, Label: in.Label, Type: in.Type, Items: in.Items, Secret: in.Secret, Required: in.Required}
	}
	for i, ev := range m.Events {
		e.Events[i] = entity.HookEvent{Name: ev.Name, Description: ev.Description}
	}
	return e
}

func nameTaken(name string) error {
	return &entity.HookError{Kind: entity.HookConflict,
		Message: fmt.Sprintf("there is already a hook %q; upgrade it instead", name)}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
