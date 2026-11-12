// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

const (
	// maxTokenFailures is how many requests with a wrong token, or to no
	// registration, an address may send in a minute before it is refused
	// without the bcrypt compare, which is what costs.
	maxTokenFailures = 10
	// maxRegistrationRequests is how many requests one registration takes in
	// a minute, so one noisy repository cannot flood the account's tasks.
	maxRegistrationRequests = 120
	// maxQueued is how many of a registration's requests may wait, so a
	// flood cannot fill the database.
	maxQueued = 1000

	// lease is how long a worker owns a delivery it claimed; a crashed
	// worker's is claimed again after it.
	lease        = time.Minute
	pollInterval = 5 * time.Second
	sweepEvery   = time.Hour
	claimBatch   = 32
	maxRounds    = 10
	workers      = 4
	// verifiedCap bounds the remembered token checks.
	verifiedCap = 4096
)

// backoff is the wait after each failed attempt; past the last, the delivery
// fails. A hook's own wrong answer fails at once: it would give it again.
var backoff = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

var statusNames = map[int16]string{
	model.HookDeliveryQueued: "queued", model.HookDeliveryProcessing: "processing", model.HookDeliveryRouted: "routed",
	model.HookDeliveryPublished: "published", model.HookDeliveryRejected: "rejected", model.HookDeliveryIgnored: "ignored",
	model.HookDeliveryDuplicate: "duplicate", model.HookDeliveryFailed: "failed",
}

// queue is the ingress's and the workers' state on this instance.
type queue struct {
	wake     chan struct{}
	failures *windows
	requests *windows

	mu sync.Mutex
	// verified remembers token checks that passed, by the stored hash and
	// the token, so a rotation, which changes the hash, forgets them.
	verified map[[32]byte]struct{}
}

func newQueue() *queue {
	return &queue{
		wake:     make(chan struct{}, 1),
		failures: newWindows(maxTokenFailures, time.Minute),
		requests: newWindows(maxRegistrationRequests, time.Minute),
		verified: map[[32]byte]struct{}{},
	}
}

func throttled(format string, a ...any) error {
	return &entity.HookError{Kind: entity.HookThrottled, Message: fmt.Sprintf(format, a...)}
}

// Receive checks a request to a hook URL and queues it, returning the
// delivery's id. An unknown or turned off registration and a wrong token are
// all ErrNotFound, so a guessed id tells nothing.
func (c *controller) Receive(ctx context.Context, in entity.HookIncoming) (int64, error) {
	if len(in.Body) > MaxRequestBodySize {
		return 0, &entity.HookError{Kind: entity.HookTooLarge, Message: fmt.Sprintf("the body is over %d bytes", MaxRequestBodySize)}
	}
	now := c.now()
	if c.q.failures.full(in.Peer, now) {
		return 0, throttled("too many requests with a wrong token")
	}
	reg, err := c.repo.GetHookRegistrationByID(ctx, in.RegistrationID)
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		return 0, err
	}
	if err != nil || !reg.Enabled || !c.tokenMatches(reg.TokenHash, in.Token) {
		c.q.failures.add(in.Peer, now)
		return 0, base.ErrNotFound
	}
	if !c.q.requests.add(monoflake.ID(reg.ID).String(), now) {
		return 0, throttled("too many requests to this hook URL")
	}
	queued, err := c.repo.CountQueuedHookDeliveries(ctx, reg.ID)
	if err != nil {
		return 0, err
	}
	if queued >= maxQueued {
		return 0, throttled("too many requests to this hook URL are waiting")
	}
	raw, _ := json.Marshal(hookrunner.Request{Method: in.Method, Path: in.Path, Query: in.Query, Headers: in.Headers, Body: in.Body}) // plain data
	sum := sha256.Sum256(in.Body)
	d := model.HookDelivery{
		ID: c.ids.NextID(), RegistrationID: reg.ID, UserID: reg.UserID, ReceivedAt: now, Status: model.HookDeliveryQueued,
		NextAttemptAt: now, Request: raw, BodySHA256: hex.EncodeToString(sum[:]), EventNames: "[]",
	}
	if err := c.repo.CreateHookDelivery(ctx, d); err != nil {
		return 0, err
	}
	c.nudge()
	c.emitAction(ctx, entity.ActionHookReceive, entity.ActorHook, reg.UserID, reg.ID)
	return d.ID, nil
}

// tokenMatches compares a token with its bcrypt hash, remembering a match:
// the compare is slow on purpose, and a provider sends many requests.
func (c *controller) tokenMatches(hash, token string) bool {
	key := sha256.Sum256([]byte(hash + "\x00" + token))
	c.q.mu.Lock()
	_, ok := c.q.verified[key]
	c.q.mu.Unlock()
	if ok {
		return true
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) != nil {
		return false
	}
	c.q.mu.Lock()
	if len(c.q.verified) >= verifiedCap {
		clear(c.q.verified)
	}
	c.q.verified[key] = struct{}{}
	c.q.mu.Unlock()
	return true
}

func (c *controller) nudge() {
	select {
	case c.q.wake <- struct{}{}:
	default:
	}
}

// ListDeliveries returns a page of a registration's log, newest first.
func (c *controller) ListDeliveries(ctx context.Context, userID, registrationID, beforeID int64, limit int) ([]entity.HookDelivery, error) {
	if _, err := c.repo.GetHookRegistration(ctx, registrationID, userID); err != nil {
		return nil, err
	}
	ds, err := c.repo.ListHookDeliveries(ctx, registrationID, userID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]entity.HookDelivery, len(ds))
	for i, d := range ds {
		names := []string{}
		_ = json.Unmarshal([]byte(d.EventNames), &names) // written by the queue
		out[i] = entity.HookDelivery{
			ID: d.ID, RegistrationID: d.RegistrationID, ReceivedAt: d.ReceivedAt, Status: statusNames[d.Status],
			Attempts: int(d.Attempts), EventNames: names, Error: d.Error, DurationMs: int(d.DurationMs),
			Replayable: d.Status == model.HookDeliveryFailed,
		}
	}
	return out, nil
}

// ReplayDelivery queues a failed delivery again, unless its registration is
// turned off.
func (c *controller) ReplayDelivery(ctx context.Context, userID, registrationID, id int64) error {
	reg, err := c.repo.GetHookRegistration(ctx, registrationID, userID)
	if err != nil {
		return err
	}
	if !reg.Enabled {
		return &entity.HookError{Kind: entity.HookConflict, Message: "the registration is turned off; turn it on to replay its deliveries"}
	}
	if err := c.repo.ReplayHookDelivery(ctx, id, registrationID, userID, c.now()); err != nil {
		return err
	}
	c.nudge()
	return nil
}

// Start runs this instance's workers, and the sweep that deletes deliveries
// past the retention, until ctx ends.
func (c *controller) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			c.drain(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			case <-c.q.wake:
			}
		}
	}()
	go func() {
		t := time.NewTicker(sweepEvery)
		defer t.Stop()
		for {
			c.sweep(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (c *controller) sweep(ctx context.Context) {
	if err := c.repo.DeleteHookDeliveriesBefore(ctx, c.now().Add(-c.retention)); err != nil {
		zlog.Error().Err(err).Msg("hooks: delete old deliveries")
	}
}

// drain works the deliveries that are due, a batch at a time. Rounds are
// bounded, so a row that cannot be written is not retried in a tight loop.
func (c *controller) drain(ctx context.Context) {
	for range maxRounds {
		due, err := c.repo.ListDueHookDeliveries(ctx, c.now(), claimBatch)
		if err != nil {
			zlog.Error().Err(err).Msg("hooks: list due deliveries")
			return
		}
		var wg sync.WaitGroup
		slots := make(chan struct{}, workers)
		for _, d := range due {
			slots <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-slots; wg.Done() }()
				c.work(ctx, d)
			}()
		}
		wg.Wait()
		if len(due) < claimBatch {
			return
		}
	}
}

// work claims a delivery and takes it a step on: a routed one is published,
// any other run through its hook.
func (c *controller) work(ctx context.Context, d model.HookDelivery) {
	now := c.now()
	claim := c.repo.ClaimHookDelivery
	if d.Status == model.HookDeliveryRouted {
		claim = c.repo.ClaimRoutedHookDelivery
	}
	ok, err := claim(ctx, d.ID, now, now.Add(lease))
	if err != nil {
		zlog.Error().Err(err).Int64("delivery", d.ID).Msg("hooks: claim")
	}
	if !ok {
		return
	}
	if d.Status == model.HookDeliveryRouted {
		c.publish(ctx, d.ID)
		return
	}
	c.process(ctx, d.ID)
}

// process runs a claimed delivery through its hook and commits the events it
// routes. Whatever stops it short leaves the lease to expire, so another
// attempt picks it up.
func (c *controller) process(ctx context.Context, id int64) {
	d, err := c.repo.GetHookDelivery(ctx, id)
	if err != nil {
		zlog.Error().Err(err).Int64("delivery", id).Msg("hooks: read delivery")
		return
	}
	start := c.now()
	reg, err := c.repo.GetHookRegistrationByID(ctx, d.RegistrationID)
	if err != nil {
		// A registration deleted takes its deliveries with it.
		c.retry(ctx, d, start, err)
		return
	}
	if !reg.Enabled {
		c.fail(ctx, d, start, "its registration is turned off")
		return
	}
	h, err := c.usableHook(ctx, reg.UserID, reg.HookID, false)
	if errors.Is(err, base.ErrNotFound) {
		c.fail(ctx, d, start, "its hook is gone or turned off")
		return
	}
	if err != nil {
		c.retry(ctx, d, start, err)
		return
	}
	inputs, err := c.inputsOf(reg)
	if err != nil {
		c.fail(ctx, d, start, "its inputs cannot be read; save the registration again")
		return
	}
	var req hookrunner.Request
	_ = json.Unmarshal(d.Request, &req) // written by Receive
	var sum [32]byte
	_, _ = hex.Decode(sum[:], []byte(h.SHA256)) // written by the upload, from a digest
	res, err := c.runner.Handle(ctx, hookrunner.Module{SHA256: sum, Load: func(context.Context) ([]byte, error) {
		return c.blobs.LoadRaw(h.BlobKey)
	}}, manifestOf(h), hookrunner.HandleInput{
		RegistrationID: monoflake.ID(reg.ID).String(), Request: req, Inputs: inputs, Now: start,
	})
	var outErr *hookrunner.OutputError
	switch {
	case errors.As(err, &outErr):
		c.fail(ctx, d, start, err.Error())
		return
	case err != nil:
		c.retry(ctx, d, start, err)
		return
	case !res.Accept:
		c.finish(ctx, d, start, model.HookDeliveryRejected, res.Reason)
		c.emitAction(ctx, entity.ActionHookReject, entity.ActorHook, d.UserID, d.RegistrationID)
		return
	}
	routes := routesOf(reg)
	var events []model.HookPendingEvent
	for _, e := range res.Events {
		eventID, ok := routes[e.Name]
		if !ok {
			continue
		}
		faq := make([]model.HookPendingFAQ, len(e.FAQ))
		for i, f := range e.FAQ {
			faq[i] = model.HookPendingFAQ{Q: f.Q, A: f.A}
		}
		events = append(events, model.HookPendingEvent{HookEvent: e.Name, EventID: eventID, DedupKey: e.DedupKey, Payload: e.Payload, FAQ: faq})
	}
	if len(events) == 0 {
		reason := res.Reason
		if reason == "" && len(res.Events) > 0 {
			reason = "none of its events is routed"
		}
		c.finish(ctx, d, start, model.HookDeliveryIgnored, reason)
		return
	}
	d.Error, d.DurationMs = "", c.since(start)
	kept, err := c.repo.RouteHookDelivery(ctx, d, events)
	if err != nil {
		c.retry(ctx, d, start, err)
		return
	}
	if len(kept) > 0 {
		c.publish(ctx, d.ID)
	}
}

// publish fires a routed delivery's pending events, then marks it published.
// Every event is resolved before any is fired, so a failure leaves nothing
// half done for the lease's end to repeat.
func (c *controller) publish(ctx context.Context, id int64) {
	d, err := c.repo.GetHookDelivery(ctx, id)
	if err != nil {
		zlog.Error().Err(err).Int64("delivery", id).Msg("hooks: read routed delivery")
		return
	}
	var pending []model.HookPendingEvent
	_ = json.Unmarshal(d.Pending, &pending) // written by RouteHookDelivery
	payloads := make([]entity.EventPublishedPayload, 0, len(pending))
	for _, e := range pending {
		ev, err := c.events.GetEvent(ctx, e.EventID, d.UserID)
		if errors.Is(err, base.ErrNotFound) {
			continue // deleted since it was routed
		}
		if err != nil {
			zlog.Error().Err(err).Int64("delivery", id).Msg("hooks: read event")
			return
		}
		faq := make([]entity.EventFAQ, len(e.FAQ))
		for i, f := range e.FAQ {
			faq[i] = entity.EventFAQ{Q: f.Q, A: f.A}
		}
		payloads = append(payloads, entity.EventPublishedPayload{EventID: ev.ID, Name: ev.Name, Payload: e.Payload, FAQ: faq})
	}
	for _, p := range payloads {
		if _, err := c.pubsub.Publish(ctx, pubsub.PublishRequest{PubSubID: entity.PubSubTopicEvents, Event: p}); err != nil {
			zlog.Error().Err(err).Int64("delivery", id).Msg("hooks: publish event")
		}
	}
	d.Status, d.Pending = model.HookDeliveryPublished, nil
	if err := c.repo.UpdateHookDelivery(ctx, d); err != nil {
		zlog.Error().Err(err).Int64("delivery", id).Msg("hooks: mark published")
		return
	}
	c.emitAction(ctx, entity.ActionHookPublish, entity.ActorHook, d.UserID, d.RegistrationID)
}

// retry queues a delivery for its next attempt, or fails it past the last.
func (c *controller) retry(ctx context.Context, d model.HookDelivery, start time.Time, cause error) {
	zlog.Warn().Err(cause).Int64("delivery", d.ID).Int16("attempt", d.Attempts).Msg("hooks: attempt failed")
	if int(d.Attempts) > len(backoff) {
		c.fail(ctx, d, start, cause.Error())
		return
	}
	d.Status, d.NextAttemptAt, d.Error, d.DurationMs = model.HookDeliveryQueued, c.now().Add(backoff[d.Attempts-1]), cause.Error(), c.since(start)
	c.saveDelivery(ctx, d)
}

// fail ends a delivery as failed, keeping its request so it can be replayed.
func (c *controller) fail(ctx context.Context, d model.HookDelivery, start time.Time, reason string) {
	d.Status, d.Error, d.DurationMs = model.HookDeliveryFailed, reason, c.since(start)
	c.saveDelivery(ctx, d)
}

// finish ends a delivery the hook decided, clearing its request.
func (c *controller) finish(ctx context.Context, d model.HookDelivery, start time.Time, status int16, reason string) {
	d.Status, d.Request, d.Error, d.DurationMs = status, nil, reason, c.since(start)
	c.saveDelivery(ctx, d)
}

func (c *controller) saveDelivery(ctx context.Context, d model.HookDelivery) {
	if err := c.repo.UpdateHookDelivery(ctx, d); err != nil {
		zlog.Error().Err(err).Int64("delivery", d.ID).Msg("hooks: save delivery")
	}
}

func (c *controller) since(start time.Time) int32 {
	return int32(c.now().Sub(start).Milliseconds())
}

// windows counts events per key in fixed windows, up to a limit.
type windows struct {
	limit int
	per   time.Duration
	mu    sync.Mutex
	m     map[string]*window
}

type window struct {
	start time.Time
	n     int
}

func newWindows(limit int, per time.Duration) *windows {
	return &windows{limit: limit, per: per, m: map[string]*window{}}
}

// current returns key's window at now, starting a new one when the last has
// ended. Ended windows are dropped once there are many keys.
func (w *windows) current(key string, now time.Time) *window {
	win, ok := w.m[key]
	if !ok || now.Sub(win.start) >= w.per {
		if len(w.m) >= verifiedCap {
			for k, old := range w.m {
				if now.Sub(old.start) >= w.per {
					delete(w.m, k)
				}
			}
		}
		win = &window{start: now}
		w.m[key] = win
	}
	return win
}

// full reports whether key has reached the limit.
func (w *windows) full(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current(key, now).n >= w.limit
}

// add counts one for key, and reports whether it was within the limit.
func (w *windows) add(key string, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	win := w.current(key, now)
	win.n++
	return win.n <= w.limit
}
