// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/mustafaturan/monoflake"
	"golang.org/x/crypto/bcrypt"
)

const (
	// TokenParam is the query parameter a hook URL carries its token in.
	TokenParam = entity.HookTokenParam
	tokenLen   = 8
	tokenChars = "0123456789abcdefghijklmnopqrstuvwxyz"

	maxRegistrationName = 255
	maxInputString      = 4096
	maxInputItems       = 64
)

// Events is what the controller reads of the account's events.
type Events interface {
	GetEvent(ctx context.Context, id int64, userID int64) (model.Event, error)
	ListEventTriggersByEvent(ctx context.Context, eventID int64, userID int64) ([]model.EventTrigger, error)
}

// Publisher carries the telemetry a registration emits.
type Publisher interface {
	Publish(ctx context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error)
}

// ListAvailable returns the hooks the account may register: the built-ins
// turned on, and its own while user hooks are allowed.
func (c *controller) ListAvailable(ctx context.Context, userID int64) ([]entity.Hook, error) {
	hooks, err := c.repo.ListHooks(ctx, builtIn)
	if err != nil {
		return nil, err
	}
	if c.allowUserHooks && userID != builtIn {
		own, err := c.repo.ListHooks(ctx, userID)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, own...)
	}
	out := []entity.Hook{}
	for _, h := range hooks {
		if h.Enabled {
			out = append(out, toEntity(h))
		}
	}
	return out, nil
}

func (c *controller) ListRegistrations(ctx context.Context, userID int64) ([]entity.HookRegistration, error) {
	regs, err := c.repo.ListHookRegistrations(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.HookRegistration, 0, len(regs))
	hooks := map[int64]model.Hook{}
	for _, reg := range regs {
		h, ok := hooks[reg.HookID]
		if !ok {
			if h, err = c.usableHook(ctx, userID, reg.HookID, true); err != nil && !errors.Is(err, base.ErrNotFound) {
				return nil, err
			}
			hooks[reg.HookID] = h
		}
		e, err := c.toRegistration(ctx, reg, h, "")
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (c *controller) GetRegistration(ctx context.Context, userID, id int64) (entity.HookRegistration, error) {
	reg, err := c.repo.GetHookRegistration(ctx, id, userID)
	if err != nil {
		return entity.HookRegistration{}, err
	}
	h, err := c.usableHook(ctx, userID, reg.HookID, true)
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		return entity.HookRegistration{}, err
	}
	return c.toRegistration(ctx, reg, h, "")
}

// CreateRegistration registers a hook in the account. The result carries the
// URL with its token: the only time it is shown.
func (c *controller) CreateRegistration(ctx context.Context, userID int64, req entity.HookRegistrationRequest) (entity.HookRegistration, error) {
	h, err := c.usableHook(ctx, userID, req.HookID, false)
	if errors.Is(err, base.ErrNotFound) {
		return entity.HookRegistration{}, invalid("there is no hook %s to register", monoflake.ID(req.HookID).String())
	}
	if err != nil {
		return entity.HookRegistration{}, err
	}
	reg := model.HookRegistration{ID: c.ids.NextID(), UserID: userID, HookID: h.ID, Enabled: true}
	if req.Enabled != nil {
		reg.Enabled = *req.Enabled
	}
	if err := c.apply(ctx, &reg, h, req, nil); err != nil {
		return entity.HookRegistration{}, err
	}
	token, hash := newToken()
	reg.TokenHash = hash
	if err := c.repo.CreateHookRegistration(ctx, reg); err != nil {
		return entity.HookRegistration{}, err
	}
	c.emitAction(ctx, entity.ActionHookRegister, entity.ActorHuman, userID, reg.ID)
	return c.toRegistration(ctx, reg, h, token)
}

func (c *controller) UpdateRegistration(ctx context.Context, userID, id int64, req entity.HookRegistrationRequest) (entity.HookRegistration, error) {
	reg, err := c.repo.GetHookRegistration(ctx, id, userID)
	if err != nil {
		return entity.HookRegistration{}, err
	}
	h, err := c.usableHook(ctx, userID, reg.HookID, true)
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		return entity.HookRegistration{}, err
	}
	// Turning a registration off or renaming it needs no usable hook, so it
	// keeps its delivery log; turning it on or changing inputs or routes does.
	if (req.Enabled == nil || !*req.Enabled) && req.Inputs == nil && req.Routes == nil {
		if req.Enabled != nil {
			reg.Enabled = false
		}
		if err := applyName(&reg, req.Name); err != nil {
			return entity.HookRegistration{}, err
		}
		if err := c.repo.UpdateHookRegistration(ctx, reg); err != nil {
			return entity.HookRegistration{}, err
		}
		return c.toRegistration(ctx, reg, h, "")
	}
	if !h.Enabled {
		return entity.HookRegistration{}, invalid("its hook is gone or turned off; delete this registration instead")
	}
	old, err := c.inputsOf(reg)
	if err != nil {
		return entity.HookRegistration{}, err
	}
	if req.Enabled != nil {
		reg.Enabled = *req.Enabled
	}
	if req.Routes == nil {
		req.Routes = routesOf(reg)
	}
	if req.Inputs == nil {
		req.Inputs = old
	}
	if err := c.apply(ctx, &reg, h, req, old); err != nil {
		return entity.HookRegistration{}, err
	}
	if err := c.repo.UpdateHookRegistration(ctx, reg); err != nil {
		return entity.HookRegistration{}, err
	}
	return c.toRegistration(ctx, reg, h, "")
}

func (c *controller) DeleteRegistration(ctx context.Context, userID, id int64) error {
	return c.repo.DeleteHookRegistration(ctx, id, userID)
}

// RotateToken gives a registration a new token, so the old URL stops working
// at once. The result carries the new URL: the only time it is shown.
func (c *controller) RotateToken(ctx context.Context, userID, id int64) (entity.HookRegistration, error) {
	reg, err := c.repo.GetHookRegistration(ctx, id, userID)
	if err != nil {
		return entity.HookRegistration{}, err
	}
	token, hash := newToken()
	if err := c.repo.SetHookRegistrationTokenHash(ctx, id, userID, hash); err != nil {
		return entity.HookRegistration{}, err
	}
	h, err := c.usableHook(ctx, userID, reg.HookID, true)
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		return entity.HookRegistration{}, err
	}
	return c.toRegistration(ctx, reg, h, token)
}

// usableHook returns a hook the account may use: a built-in, or its own while
// user hooks are allowed; turned on, unless anyState. Anything else is
// ErrNotFound.
func (c *controller) usableHook(ctx context.Context, userID, hookID int64, anyState bool) (model.Hook, error) {
	h, err := c.repo.GetHook(ctx, hookID, builtIn)
	if errors.Is(err, base.ErrNotFound) && c.allowUserHooks {
		h, err = c.repo.GetHook(ctx, hookID, userID)
	}
	if err != nil {
		return model.Hook{}, err
	}
	if !h.Enabled && !anyState {
		return model.Hook{}, base.ErrNotFound
	}
	return h, nil
}

// apply checks the name, inputs and routes asked for against the hook and
// the account's events, and sets them on reg. old is the inputs stored
// before, whose secrets an update may leave out.
func (c *controller) apply(ctx context.Context, reg *model.HookRegistration, h model.Hook, req entity.HookRegistrationRequest, old map[string]any) error {
	if err := applyName(reg, req.Name); err != nil {
		return err
	}
	m := manifestOf(h)
	inputs, err := checkInputs(m, req.Inputs, old)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(inputs) // checked values: strings, numbers, booleans and lists of them
	sealed, nonce, err := security.Encrypt(string(b), c.tokenKey)
	if err != nil {
		return fmt.Errorf("hook: seal inputs: %w", err)
	}
	reg.Inputs = nonce + ":" + sealed
	for name, eventID := range req.Routes {
		if !m.Declares(name) {
			return invalid("the hook %q writes no event %q", h.Name, name)
		}
		if _, err := c.events.GetEvent(ctx, eventID, reg.UserID); errors.Is(err, base.ErrNotFound) {
			return invalid("the event %s routed from %q is not one of this account's", monoflake.ID(eventID).String(), name)
		} else if err != nil {
			return err
		}
	}
	b, _ = json.Marshal(req.Routes)
	reg.Routes = string(b)
	return nil
}

// applyName sets the name asked for, if any, and checks the result.
func applyName(reg *model.HookRegistration, name *string) error {
	if name != nil {
		reg.Name = strings.TrimSpace(*name)
	}
	if reg.Name == "" || utf8.RuneCountInString(reg.Name) > maxRegistrationName {
		return invalid("a name of 1 to %d characters is required", maxRegistrationName)
	}
	return nil
}

// checkInputs keeps the values the manifest declares, each of its type, with
// every required one set. A secret left out keeps its value in old.
func checkInputs(m hookrunner.Manifest, in, old map[string]any) (map[string]any, error) {
	out := map[string]any{}
	declared := map[string]bool{}
	for _, def := range m.Inputs {
		declared[def.Name] = true
		v, ok := in[def.Name]
		if !ok && def.Secret {
			v, ok = old[def.Name]
		}
		if ok && v != nil {
			if err := checkValue(def, v); err != nil {
				return nil, err
			}
			if s, isString := v.(string); !isString || s != "" {
				out[def.Name] = v
			}
		}
		if _, set := out[def.Name]; def.Required && !set {
			return nil, invalid("the input %q is required", def.Name)
		}
	}
	for name := range in {
		if !declared[name] {
			return nil, invalid("the hook has no input %q", name)
		}
	}
	return out, nil
}

func checkValue(def hookrunner.Input, v any) error {
	switch def.Type {
	case hookrunner.InputArray:
		items, ok := v.([]any)
		if !ok || len(items) > maxInputItems {
			return invalid("the input %q must be a list of at most %d", def.Name, maxInputItems)
		}
		for _, item := range items {
			if err := checkValue(hookrunner.Input{Name: def.Name, Type: def.Items}, item); err != nil {
				return err
			}
		}
		return nil
	case hookrunner.InputNumber:
		if f, ok := v.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
			return nil
		}
	case hookrunner.InputBoolean:
		if _, ok := v.(bool); ok {
			return nil
		}
	default:
		if s, ok := v.(string); ok && len(s) <= maxInputString {
			return nil
		}
	}
	return invalid("the input %q must be a %s", def.Name, def.Type)
}

// inputsOf opens a registration's stored inputs.
func (c *controller) inputsOf(reg model.HookRegistration) (map[string]any, error) {
	nonce, sealed, _ := strings.Cut(reg.Inputs, ":")
	plain, err := security.Decrypt(sealed, c.tokenKey, nonce)
	if err != nil {
		return nil, fmt.Errorf("hook: open inputs of registration %d: %w", reg.ID, err)
	}
	var inputs map[string]any
	_ = json.Unmarshal([]byte(plain), &inputs) // sealed by apply, from a JSON object
	return inputs, nil
}

func routesOf(reg model.HookRegistration) map[string]int64 {
	routes := map[string]int64{}
	_ = json.Unmarshal([]byte(reg.Routes), &routes) // written by apply
	return routes
}

// toRegistration renders a registration: its secret inputs only as set, and
// its URL with the token only when token is given.
func (c *controller) toRegistration(ctx context.Context, reg model.HookRegistration, h model.Hook, token string) (entity.HookRegistration, error) {
	inputs, err := c.inputsOf(reg)
	if err != nil {
		return entity.HookRegistration{}, err
	}
	e := entity.HookRegistration{
		ID: reg.ID, HookID: reg.HookID, HookName: h.Name, HookAvailable: h.ID != 0 && h.Enabled,
		Name: reg.Name, Enabled: reg.Enabled, Inputs: map[string]any{}, SecretsSet: []string{},
		Routes: routesOf(reg), URL: c.baseURL + "/api/v1/hooks/in/" + monoflake.ID(reg.ID).String(),
		CreatedAt: reg.CreatedAt, UpdatedAt: reg.UpdatedAt,
	}
	if token != "" {
		e.URLWithToken = e.URL + "?" + TokenParam + "=" + token
	}
	secret := map[string]bool{}
	for _, def := range manifestOf(h).Inputs {
		secret[def.Name] = def.Secret
	}
	for name, v := range inputs {
		if secret[name] {
			e.SecretsSet = append(e.SecretsSet, name)
		} else {
			e.Inputs[name] = v
		}
	}
	slices.Sort(e.SecretsSet)
	// A trigger that runs every command without asking, fed by text whoever
	// can send the webhook writes, is worth a warning.
	for name, eventID := range e.Routes {
		triggers, err := c.events.ListEventTriggersByEvent(ctx, eventID, reg.UserID)
		if err != nil {
			return entity.HookRegistration{}, err
		}
		if slices.ContainsFunc(triggers, func(t model.EventTrigger) bool { return t.AllowAllCommands }) {
			e.AllowAllCommandsEvents = append(e.AllowAllCommandsEvents, name)
		}
	}
	slices.Sort(e.AllowAllCommandsEvents)
	return e, nil
}

// emitAction sends a registration's telemetry; nil PubSub sends none.
func (c *controller) emitAction(ctx context.Context, action entity.Action, actor entity.Actor, userID, registrationID int64) {
	if c.pubsub == nil {
		return
	}
	origin := entity.GetOrigin(ctx)
	if origin == entity.OriginInvalid {
		origin = entity.OriginAPI
	}
	_, _ = c.pubsub.Publish(ctx, pubsub.PublishRequest{PubSubID: entity.PubSubTopicCRUD, Event: entity.CRUDEvent{
		Action: action, UserID: userID, ResourceType: entity.ResourceHookRegistration,
		ResourceID: registrationID, Actor: actor, Origin: origin,
	}})
}

// newToken returns a fresh agentrq_hook_token and its bcrypt hash. Each
// character is drawn uniformly: a byte from 252 up, past the last whole
// multiple of 36, is drawn again.
func newToken() (string, string) {
	b := make([]byte, 0, tokenLen)
	var one [1]byte
	for len(b) < tokenLen {
		_, _ = rand.Read(one[:]) // crypto/rand never fails
		if int(one[0]) < 256/len(tokenChars)*len(tokenChars) {
			b = append(b, tokenChars[int(one[0])%len(tokenChars)])
		}
	}
	// bcrypt refuses only a password over 72 bytes.
	hash, _ := bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
	return string(b), string(hash)
}

func invalid(format string, a ...any) error {
	return &entity.HookError{Kind: entity.HookInvalid, Message: fmt.Sprintf(format, a...)}
}
