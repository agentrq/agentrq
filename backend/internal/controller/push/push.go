// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

type (
	Config struct {
		VAPIDPublicKey  string `yaml:"vapidPublicKey"`
		VAPIDPrivateKey string `yaml:"vapidPrivateKey"`
		Subscriber      string `yaml:"subscriber"`
	}

	Params struct {
		Config     Config
		Repository base.Repository
		PubSub     pubsub.Service
		IDGen      idgen.Service
		// BasePath is cfg.App.BasePath — the reverse-proxy prefix the SPA is
		// served under (e.g. /abc/def), or "" at the root. A notification is
		// opened by the service worker via clients.openWindow, which never
		// goes through Vue Router and has no way to learn this on its own, so
		// the backend has to bake it into the URL it sends.
		BasePath string
	}

	Controller interface {
		Start(ctx context.Context) error
		SaveSubscription(ctx context.Context, req entity.SavePushSubscriptionRequest) error
		DeleteSubscription(ctx context.Context, req entity.DeletePushSubscriptionRequest) error
		DeleteSubscriptionByWorkspace(ctx context.Context, req entity.DeletePushSubscriptionByWorkspaceRequest) error
		CheckSubscription(ctx context.Context, req entity.CheckPushSubscriptionRequest) (bool, error)
		VAPIDPublicKey() string
		IsEnabled() bool
	}

	controller struct {
		cfg      Config
		repo     base.Repository
		pubsub   pubsub.Service
		ids      idgen.Service
		basePath string
	}

	pushPayload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		URL   string `json:"url"`
		Tag   string `json:"tag"`
	}
)

const (
	PushTypeTaskCreate    = "task_create"
	PushTypeTaskUpdate    = "task_update"
	PushTypeMessageCreate = "message_create"
)

func New(p Params) Controller {
	return &controller{
		cfg:      p.Config,
		repo:     p.Repository,
		pubsub:   p.PubSub,
		ids:      p.IDGen,
		basePath: p.BasePath,
	}
}

func (c *controller) IsEnabled() bool {
	return c.cfg.VAPIDPublicKey != "" && c.cfg.VAPIDPrivateKey != ""
}

func (c *controller) VAPIDPublicKey() string {
	return c.cfg.VAPIDPublicKey
}

func (c *controller) SaveSubscription(ctx context.Context, req entity.SavePushSubscriptionRequest) error {
	sub := model.PushSubscription{
		ID:          c.ids.NextID(),
		UserID:      req.UserID,
		WorkspaceID: req.WorkspaceID,
		Endpoint:    req.Endpoint,
		P256dh:      req.P256dh,
		Auth:        req.Auth,
		UserAgent:   req.UserAgent,
		Types:       strings.Join(req.Types, ","),
	}
	return c.repo.SavePushSubscription(ctx, sub)
}

func (c *controller) DeleteSubscription(ctx context.Context, req entity.DeletePushSubscriptionRequest) error {
	return c.repo.DeletePushSubscription(ctx, req.UserID, req.Endpoint)
}

func (c *controller) DeleteSubscriptionByWorkspace(ctx context.Context, req entity.DeletePushSubscriptionByWorkspaceRequest) error {
	return c.repo.DeletePushSubscriptionByWorkspace(ctx, req.UserID, req.WorkspaceID, req.Endpoint)
}

func (c *controller) CheckSubscription(ctx context.Context, req entity.CheckPushSubscriptionRequest) (bool, error) {
	return c.repo.GetPushSubscriptionForWorkspace(ctx, req.UserID, req.WorkspaceID, req.Endpoint)
}

func (c *controller) Start(ctx context.Context) error {
	if !c.IsEnabled() {
		zlog.Info().Msg("[push] VAPID keys not configured — push notifications disabled")
		return nil
	}

	res, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD})
	if err != nil {
		return fmt.Errorf("failed to subscribe to CRUD topic: %w", err)
	}

	zlog.Info().Msg("[push] started controller")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-res.Events:
				if !ok {
					zlog.Warn().Msg("[push] pubsub channel closed")
					return
				}
				ev, ok := msg.(entity.CRUDEvent)
				if !ok {
					continue
				}
				go c.processEvent(context.Background(), ev)
			}
		}
	}()

	return nil
}

func (c *controller) processEvent(ctx context.Context, ev entity.CRUDEvent) {
	ws, err := c.repo.SystemGetWorkspace(ctx, ev.WorkspaceID)
	if err != nil {
		return
	}
	ownerID := ws.UserID

	workspaceIDStr := monoflake.ID(ev.WorkspaceID).String()

	switch ev.ResourceType {
	case entity.ResourceTask:
		t, err := c.repo.SystemGetTask(ctx, ev.ResourceID)
		if err != nil {
			return
		}
		// Only notify the workspace owner when the agent creates/updates a task
		if ev.Actor != entity.ActorAgent {
			return
		}
		switch ev.Action {
		case entity.ActionTaskCreate:
			c.sendToUser(ctx, ownerID, ev.WorkspaceID, ws.ContentID(), PushTypeTaskCreate, taskCreatePayload(c.basePath, ws.Name, workspaceIDStr, t))
		case entity.ActionTaskUpdate, entity.ActionTaskComplete:
			c.sendToUser(ctx, ownerID, ev.WorkspaceID, ws.ContentID(), PushTypeTaskUpdate, taskStatusPayload(c.basePath, ws.Name, workspaceIDStr, t))
		}
		return

	case entity.ResourceMessage:
		if ev.Action != entity.ActionMessageCreate {
			return
		}
		m, err := c.repo.SystemGetMessage(ctx, ev.ResourceID)
		if err != nil {
			return
		}
		// Only notify on agent replies, and not on the agent's own telemetry
		if m.Sender != "agent" || isSilentTelemetry(m) {
			return
		}
		t, err := c.repo.SystemGetTask(ctx, m.TaskID)
		if err != nil {
			return
		}
		c.sendToUser(ctx, ownerID, ev.WorkspaceID, ws.ContentID(), PushTypeMessageCreate, replyPayload(c.basePath, workspaceIDStr, t, m))
		return

	default:
		return
	}
}

// silentTelemetryTypes are the agent telemetry message types that never
// notify: the agent working, not saying anything. A notice still does.
var silentTelemetryTypes = map[string]bool{
	"agent_thought": true,
	"agent_plan":    true,
	"agent_usage":   true,
}

// isSilentTelemetry reports whether m is agent telemetry that should not notify.
func isSilentTelemetry(m model.Message) bool {
	var meta struct {
		Type string `json:"type"`
	}
	if len(m.Metadata) == 0 || json.Unmarshal(m.Metadata, &meta) != nil {
		return false
	}
	return silentTelemetryTypes[meta.Type]
}

// taskURL is the path a task-related notification opens on click. It must
// point at the task, not just its workspace, or the notification is a
// dead end the user has to re-navigate from.
//
// basePath is prepended raw (no separator): it is already normalized to ""
// or a leading-slash, no-trailing-slash prefix by app.go, the same value the
// SPA itself is served under. This URL is opened by the service worker's
// clients.openWindow, a browser API with no notion of the app's router or its
// base — unlike a client-side router.push, it resolves an absolute path
// against the origin root, so a deployment served under a reverse-proxy
// prefix needs that prefix baked in here or the notification 404s.
func taskURL(basePath, workspaceIDStr string, taskID int64) string {
	return fmt.Sprintf("%s/workspaces/%s/tasks/%s", basePath, workspaceIDStr, monoflake.ID(taskID).String())
}

func taskCreatePayload(basePath, wsName, workspaceIDStr string, t model.Task) pushPayload {
	return pushPayload{
		Title: fmt.Sprintf("New task: %s", truncate(t.Title, 60)),
		Body:  wsName,
		URL:   taskURL(basePath, workspaceIDStr, t.ID),
		Tag:   fmt.Sprintf("task-create-%d", t.ID),
	}
}

func taskStatusPayload(basePath, wsName, workspaceIDStr string, t model.Task) pushPayload {
	return pushPayload{
		Title: fmt.Sprintf("Task %s: %s", strings.ToUpper(t.Status), truncate(t.Title, 50)),
		Body:  wsName,
		URL:   taskURL(basePath, workspaceIDStr, t.ID),
		Tag:   fmt.Sprintf("task-status-%d", t.ID),
	}
}

func replyPayload(basePath, workspaceIDStr string, t model.Task, m model.Message) pushPayload {
	return pushPayload{
		Title: fmt.Sprintf("Reply on: %s", truncate(t.Title, 55)),
		Body:  truncate(m.Text, 100),
		URL:   taskURL(basePath, workspaceIDStr, t.ID),
		Tag:   fmt.Sprintf("reply-%d", m.ID),
	}
}

// sendToUser notifies the user's subscriptions for workspaceID. A fork has
// none of its own — the browser subscribed on the parent's page — so when it has
// no subscription, those of fallbackID (the parent; 0 or equal means none) are used.
func (c *controller) sendToUser(ctx context.Context, userID int64, workspaceID, fallbackID int64, eventType string, payload pushPayload) {
	subs, err := c.repo.ListPushSubscriptionsByUserAndWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return
	}
	if len(subs) == 0 && fallbackID != 0 && fallbackID != workspaceID {
		subs, err = c.repo.ListPushSubscriptionsByUserAndWorkspace(ctx, userID, fallbackID)
		if err != nil {
			return
		}
	}
	if len(subs) == 0 {
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	subscriber := c.cfg.Subscriber
	if subscriber == "" {
		subscriber = "mailto:hi@example.com"
	}

	for _, sub := range subs {
		if !subscriptionAllowsType(sub.Types, eventType) {
			continue
		}
		wpSub := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.P256dh,
				Auth:   sub.Auth,
			},
		}
		resp, err := webpush.SendNotification(data, wpSub, &webpush.Options{
			VAPIDPublicKey:  c.cfg.VAPIDPublicKey,
			VAPIDPrivateKey: c.cfg.VAPIDPrivateKey,
			Subscriber:      subscriber,
			TTL:             3600,
		})
		if err != nil {
			zlog.Warn().Err(err).Str("endpoint", sub.Endpoint).Msg("[push] failed to send notification")
			continue
		}
		resp.Body.Close()

		// webpush.SendNotification only returns a non-nil error for transport-level
		// failures; a delivered-but-rejected request comes back here with err == nil and
		// the failure encoded in the status code. Per RFC 8030 §7.3 the push service
		// returns 404 Not Found or 410 Gone once a subscription no longer exists, so
		// prune it — otherwise it lingers in the DB and every future event re-attempts
		// delivery to a dead endpoint.
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			_ = c.repo.DeletePushSubscription(ctx, userID, sub.Endpoint)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// subscriptionAllowsType returns true when the subscription has opted into the
// given event type. An empty stored types string means all types are allowed.
func subscriptionAllowsType(storedTypes string, eventType string) bool {
	if storedTypes == "" {
		return true
	}
	for _, t := range strings.Split(storedTypes, ",") {
		if strings.TrimSpace(t) == eventType {
			return true
		}
	}
	return false
}
