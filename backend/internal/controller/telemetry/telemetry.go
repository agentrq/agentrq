package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm/clause"
)

type (
	Params struct {
		DB        dbconn.DBConn
		PubSub    pubsub.Service
		BatchSize int
		Interval  time.Duration
	}

	Controller interface {
		Start(ctx context.Context) error
		Close()
	}

	controller struct {
		db        dbconn.DBConn
		pubsub    pubsub.Service
		queue     chan model.Telemetry
		stop      chan struct{}
		closeOnce sync.Once
		wg        sync.WaitGroup
		batchSize int
		interval  time.Duration

		// seenClients caches MCPClient IDs already persisted so recordMCPClient
		// doesn't hit the DB for every single event from an already-known client.
		seenClientsMu sync.Mutex
		seenClients   map[uint64]struct{}
	}
)

func New(p Params) Controller {
	if p.BatchSize == 0 {
		p.BatchSize = 1000
	}
	if p.Interval == 0 {
		p.Interval = 5 * time.Second
	}

	return &controller{
		db:          p.DB,
		pubsub:      p.PubSub,
		queue:       make(chan model.Telemetry, 10000),
		stop:        make(chan struct{}),
		batchSize:   p.BatchSize,
		interval:    p.Interval,
		seenClients: make(map[uint64]struct{}),
	}
}

func (c *controller) Start(ctx context.Context) error {
	// Subscribe to Topic 0 (CRUD Events)
	crudRes, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD})
	if err != nil {
		return err
	}

	// Subscribe to Topic 2 (MCP Events)
	mcpRes, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP})
	if err != nil {
		return err
	}

	c.wg.Add(1)
	go c.worker()

	zlog.Info().Msg("[telemetry] started controller")

	// Consume CRUD Events
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
					c.recordCRUD(event)
				}
			}
		}
	}()

	// Consume MCP Events
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
					c.recordMCP(event)
				}
			}
		}
	}()

	return nil
}

func (c *controller) recordCRUD(event entity.CRUDEvent) {
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
		c.queue <- model.Telemetry{
			UserID:      event.UserID,
			WorkspaceID: event.WorkspaceID,
			OccurredAt:  time.Now().Unix(),
			Action:      model.ActionIDTaskFromScheduled,
			Actor:       uint8(event.Actor),
		}
		c.queue <- model.Telemetry{
			UserID:      event.UserID,
			WorkspaceID: event.WorkspaceID,
			OccurredAt:  time.Now().Unix(),
			Action:      model.ActionIDTaskCreate,
			Actor:       uint8(event.Actor),
		}
		return
	case entity.ActionTaskRejectManual:
		action = model.ActionIDTaskRejectManual
	case entity.ActionUserCreate:
		action = model.ActionIDUserCreate
	default:
		return
	}

	c.queue <- model.Telemetry{
		UserID:      event.UserID,
		WorkspaceID: event.WorkspaceID,
		OccurredAt:  time.Now().Unix(),
		Action:      action,
		Actor:       uint8(event.Actor),
	}
}

func (c *controller) recordMCP(event mcp.MCPEvent) {
	var action uint8
	switch event.Action {
	case mcp.ActionMCPToolCall:
		action = model.ActionIDMCPToolCall
	case mcp.ActionMCPConnect:
		action = model.ActionIDMCPConnect
	case mcp.ActionMCPNotification:
		switch event.Method {
		case "permission_manual_allow":
			action = model.ActionIDMCPPermissionManual
		case "permission_auto_allow":
			action = model.ActionIDMCPPermissionAuto
		case "permission_manual_deny":
			action = model.ActionIDMCPPermissionDeny
		default:
			return
		}
	default:
		return
	}

	c.recordMCPClient(event.ClientID, event.ClientName, event.ClientVersion)

	c.queue <- model.Telemetry{
		UserID:      event.UserID,
		WorkspaceID: event.WorkspaceID,
		OccurredAt:  time.Now().Unix(),
		Action:      action,
		Actor:       uint8(event.Actor),
		ClientID:    event.ClientID,
	}
}

// recordMCPClient upserts a lookup row for a newly-seen MCP client identity so
// Telemetry.ClientID can reference "which agent" without repeating the raw
// name/version on every row. A cache of already-persisted IDs keeps this from
// hitting the DB on every single event once a client has been seen once.
func (c *controller) recordMCPClient(id uint64, name, version string) {
	if id == 0 {
		return
	}

	c.seenClientsMu.Lock()
	_, known := c.seenClients[id]
	c.seenClientsMu.Unlock()
	if known {
		return
	}

	err := c.db.Conn(context.Background()).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.MCPClient{
		ID:      id,
		Name:    name,
		Version: version,
	}).Error
	if err != nil {
		zlog.Error().Err(err).Uint64("client_id", id).Msg("[telemetry] failed to record mcp client")
		return
	}

	c.seenClientsMu.Lock()
	c.seenClients[id] = struct{}{}
	c.seenClientsMu.Unlock()
}

func (c *controller) worker() {
	defer c.wg.Done()

	buffer := make([]model.Telemetry, 0, c.batchSize)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		if err := c.db.Conn(context.Background()).Create(&buffer).Error; err != nil {
			zlog.Error().Err(err).Msg("[telemetry] flush error")
		}
		buffer = buffer[:0]
	}

	for {
		select {
		case record := <-c.queue:
			buffer = append(buffer, record)
			if len(buffer) >= c.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-c.stop:
			// Drain anything still queued so a shutdown flush loses nothing, then flush.
			for {
				select {
				case record := <-c.queue:
					buffer = append(buffer, record)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Close stops the worker, draining and flushing any buffered telemetry so shutdown does
// not drop up to a batch interval of events. It is idempotent.
func (c *controller) Close() {
	c.closeOnce.Do(func() { close(c.stop) })
	c.wg.Wait()
}
