package pub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	zlog "github.com/rs/zerolog/log"
)

type (
	StatEntry struct {
		Action string `json:"action"`
		Total  int64  `json:"total"`
		Inc    int64  `json:"inc"`
	}

	StatsController interface {
		Start(ctx context.Context) error
		Subscribe() chan []byte
		Unsubscribe(ch chan []byte)
	}

	Params struct {
		Repository base.Repository
		PubSub     pubsub.Service
	}

	statsController struct {
		repo   base.Repository
		pubsub pubsub.Service

		mu      sync.Mutex
		totals  map[uint8]int64
		inc     map[uint8]int64
		clients map[chan []byte]struct{}

		wg   sync.WaitGroup
		stop chan struct{}
	}
)

func NewStatsController(p Params) StatsController {
	return &statsController{
		repo:    p.Repository,
		pubsub:  p.PubSub,
		totals:  make(map[uint8]int64),
		inc:     make(map[uint8]int64),
		clients: make(map[chan []byte]struct{}),
		stop:    make(chan struct{}),
	}
}

func (c *statsController) Start(ctx context.Context) error {
	zlog.Info().Msg("[pub_stats] starting stats controller")

	// 1. Load initial totals from Database
	totals, err := c.repo.GetTelemetryActionCounts(ctx)
	if err != nil {
		zlog.Error().Err(err).Msg("[pub_stats] failed to load telemetry action counts")
	} else {
		c.mu.Lock()
		c.totals = totals
		c.mu.Unlock()
		zlog.Info().Int("distinct_actions", len(totals)).Msg("[pub_stats] loaded baseline counts")
	}

	// 2. Subscribe to CRUD and MCP pubsub
	crudRes, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD})
	if err != nil {
		return err
	}
	mcpRes, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP})
	if err != nil {
		return err
	}

	c.wg.Add(1)
	go c.broadcastLoop(ctx)

	// Consume CRUD
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-crudRes.Events:
				if !ok {
					return
				}
				if event, ok := msg.(entity.CRUDEvent); ok {
					c.handleCRUD(event)
				}
			}
		}
	}()

	// Consume MCP
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-mcpRes.Events:
				if !ok {
					return
				}
				if event, ok := msg.(mcp.MCPEvent); ok {
					c.handleMCP(event)
				}
			}
		}
	}()

	return nil
}

func (c *statsController) Subscribe() chan []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan []byte, 100)
	c.clients[ch] = struct{}{}
	return ch
}

func (c *statsController) Unsubscribe(ch chan []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.clients[ch]; ok {
		delete(c.clients, ch)
		// we don't close the channel here to avoid panic on send, or we handle send safely if closed.
		// actually, safer to just delete and let garbage collection handle it since sender won't reference it anymore.
	}
}

func (c *statsController) broadcastLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stop:
			return
		case <-ticker.C:
			c.mu.Lock()

			// Always send a payload, even if increments are 0, to keep SSE stream active and updated with totals.
			// Or we could only send non-zero increments, but the user requested:
			// "publishes inc event for each action every second type only with [{"action": "<>", "total": <count>, "inc": <>}, ...]"

			// We will report ALL tracked actions in this second. If an action's total exists, include it.
			var payload []StatEntry
			for actionID, total := range c.totals {
				name := actionName(actionID)
				if name != "unknown" {
					payload = append(payload, StatEntry{
						Action: name,
						Total:  total,
						Inc:    c.inc[actionID],
					})
				}
			}

			// Reset incs
			c.inc = make(map[uint8]int64)

			// Active clients?
			if len(c.clients) > 0 && len(payload) > 0 {
				data, err := json.Marshal(payload)
				if err == nil {
					for ch := range c.clients {
						select {
						case ch <- data:
						default:
							// channel full or blocked, skip
						}
					}
				}
			}

			c.mu.Unlock()
		}
	}
}

func (c *statsController) recordAction(action uint8) {
	if action == model.ActionIDUnknown {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totals[action]++
	c.inc[action]++
}

func (c *statsController) handleCRUD(event entity.CRUDEvent) {
	var action uint8
	switch event.Action {
	case entity.ActionWorkspaceCreate:
		action = model.ActionIDWorkspaceCreate
	case entity.ActionWorkspaceUpdate:
		action = model.ActionIDWorkspaceUpdate
	case entity.ActionWorkspaceDelete:
		action = model.ActionIDWorkspaceDelete
	case entity.ActionTaskCreate:
		action = model.ActionIDTaskCreate
	case entity.ActionTaskUpdate:
		action = model.ActionIDTaskUpdate
	case entity.ActionTaskDelete:
		action = model.ActionIDTaskDelete
	case entity.ActionMessageCreate:
		action = model.ActionIDMessageCreate
	case entity.ActionMessageUpdate:
		action = model.ActionIDMessageUpdate
	case entity.ActionMessageDelete:
		action = model.ActionIDMessageDelete
	case entity.ActionTaskComplete:
		action = model.ActionIDTaskComplete
	case entity.ActionTaskApproveManual:
		action = model.ActionIDTaskApproveManual
	case entity.ActionTaskFromScheduled:
		action = model.ActionIDTaskFromScheduled
	case entity.ActionTaskRejectManual:
		action = model.ActionIDTaskRejectManual
	}
	c.recordAction(action)
}

func (c *statsController) handleMCP(event mcp.MCPEvent) {
	var action uint8
	switch event.Action {
	case mcp.ActionMCPToolCall:
		action = model.ActionIDMCPToolCall
	case mcp.ActionMCPNotification:
		switch event.Method {
		case "permission_manual_allow":
			action = model.ActionIDMCPPermissionManual
		case "permission_auto_allow":
			action = model.ActionIDMCPPermissionAuto
		case "permission_manual_deny":
			action = model.ActionIDMCPPermissionDeny
		}
	}
	c.recordAction(action)
}

func actionName(action uint8) string {
	switch action {
	case model.ActionIDWorkspaceCreate:
		return "workspace_create"
	case model.ActionIDWorkspaceUpdate:
		return "workspace_update"
	case model.ActionIDWorkspaceDelete:
		return "workspace_delete"
	case model.ActionIDTaskCreate:
		return "task_create"
	case model.ActionIDTaskUpdate:
		return "task_update"
	case model.ActionIDTaskDelete:
		return "task_delete"
	case model.ActionIDMessageCreate:
		return "message_create"
	case model.ActionIDMessageUpdate:
		return "message_update"
	case model.ActionIDMessageDelete:
		return "message_delete"
	case model.ActionIDMCPToolCall:
		return "mcp_tool_call"
	case model.ActionIDTaskApproveManual:
		return "task_approve_manual"
	case model.ActionIDMCPPermissionManual:
		return "mcp_permission_manual"
	case model.ActionIDMCPPermissionAuto:
		return "mcp_permission_auto"
	case model.ActionIDMCPPermissionDeny:
		return "mcp_permission_deny"
	case model.ActionIDTaskRejectManual:
		return "task_reject_manual"
	case model.ActionIDTaskComplete:
		return "task_complete"
	case model.ActionIDTaskFromScheduled:
		return "task_from_scheduled"
	default:
		return "unknown"
	}
}
