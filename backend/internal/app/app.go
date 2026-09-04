// Package app wires all dependencies and starts the server.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	"gorm.io/datatypes"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	eventctrl "github.com/agentrq/agentrq/backend/internal/controller/event"
	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/controller/notification"
	"github.com/agentrq/agentrq/backend/internal/controller/pub"
	pushctrl "github.com/agentrq/agentrq/backend/internal/controller/push"
	slackctrl "github.com/agentrq/agentrq/backend/internal/controller/slack"
	"github.com/agentrq/agentrq/backend/internal/controller/telemetry"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	handlerapi "github.com/agentrq/agentrq/backend/internal/handler/api"
	"github.com/agentrq/agentrq/backend/internal/handler/api/middleware/ddos"
	"github.com/agentrq/agentrq/backend/internal/handler/api/middleware/ratelimit"
	handlercoremcp "github.com/agentrq/agentrq/backend/internal/handler/coremcp"
	handlermcp "github.com/agentrq/agentrq/backend/internal/handler/mcp"
	handlerslack "github.com/agentrq/agentrq/backend/internal/handler/slack"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	repopg "github.com/agentrq/agentrq/backend/internal/repository/postgres"
	reposqlite "github.com/agentrq/agentrq/backend/internal/repository/sqlite"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/cleanup"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	svclimit "github.com/agentrq/agentrq/backend/internal/service/ratelimit"
	"github.com/agentrq/agentrq/backend/internal/service/scheduler"
	"github.com/agentrq/agentrq/backend/internal/service/server"
	slacksvc "github.com/agentrq/agentrq/backend/internal/service/slack"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/gofiber/contrib/fiberzerolog"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/mustafaturan/monoflake"
)

type (
	Config struct {
		App struct {
			Port         int    `yaml:"port"`
			SSLPort      int    `yaml:"sslPort"`
			BaseURL      string `yaml:"baseUrl"`
			Domain       string `yaml:"domain"`       // e.g. agentrq.com
			BasePath     string `yaml:"basePath"`     // e.g. /abc/def for reverse proxy
			CookieSecure string `yaml:"cookieSecure"` // "true"/"false"/"" (empty = follow SSL)
			ProxyDomain  string `yaml:"proxyDomain"`  // e.g. example.com for reverse proxy
		} `yaml:"app"`
		SSL struct {
			Enabled            bool   `yaml:"enabled"`
			CacheDir           string `yaml:"cacheDir"`
			LetsencryptEmail   string `yaml:"letsencryptEmail"`
			CloudflareAPIToken string `yaml:"cloudflareApiToken"`
		} `yaml:"ssl"`
		Auth struct {
			Google struct {
				ClientID     string `yaml:"clientId"`
				ClientSecret string `yaml:"clientSecret"`
			} `yaml:"google"`
			GitHub struct {
				ClientID     string `yaml:"clientId"`
				ClientSecret string `yaml:"clientSecret"`
			} `yaml:"github"`
			JWTSecret         string `yaml:"jwtSecret"`
			RootAccessToken   string `yaml:"rootAccessToken"`
			RootLoginEnabled  bool   `yaml:"rootLoginEnabled"`
			WorkspaceTokenKey string `yaml:"workspaceTokenKey"`
		} `yaml:"auth"`
		SMTP    smtp.Config     `yaml:"smtp"`
		Slack   slacksvc.Config `yaml:"slack"`
		WebPush pushctrl.Config `yaml:"webPush"`
		Ddos    struct {
			Enabled              bool          `yaml:"enabled"`
			MaxRequestsPerSecond int           `yaml:"maxRequestsPerSecond"`
			BlockDuration        time.Duration `yaml:"blockDuration"`
		} `yaml:"ddos"`
		Ratelimit struct {
			Enabled    bool          `yaml:"enabled"`
			MaxPerIP   int           `yaml:"maxPerIP"`
			MaxPerUser int           `yaml:"maxPerUser"`
			Window     time.Duration `yaml:"window"`
		} `yaml:"ratelimit"`
		Storage   cleanup.Config `yaml:"storage"`
		ConfigSvc config.Service `yaml:"-"` // injected, not from YAML
	}

	App struct {
		server    server.Service
		bus       *eventbus.Bus
		pubsub    pubsub.Service
		telemetry telemetry.Controller
		cancel    context.CancelFunc
	}
)

func New(cfg Config) (*App, error) {
	cfg.App.BaseURL = strings.TrimSuffix(cfg.App.BaseURL, "/")
	if cfg.App.BaseURL == "" {
		cfg.App.BaseURL = fmt.Sprintf("http://localhost:%d", cfg.App.Port)
	}

	// Normalize base path: strip trailing slash, ensure leading slash if non-empty.
	cfg.App.BasePath = strings.TrimSuffix(cfg.App.BasePath, "/")
	if cfg.App.BasePath != "" {
		if !strings.HasPrefix(cfg.App.BasePath, "/") {
			cfg.App.BasePath = "/" + cfg.App.BasePath
		}
		// Disallow ambiguous absolute-URL-like prefixes.
		if strings.HasPrefix(cfg.App.BasePath, "//") || strings.HasPrefix(cfg.App.BasePath, "/\\") {
			cfg.App.BasePath = ""
		}
	}

	// Compute effective cookie Secure flag: explicit config wins, otherwise follow SSL.
	cookieSecure := cfg.SSL.Enabled
	switch strings.ToLower(strings.TrimSpace(cfg.App.CookieSecure)) {
	case "true", "1", "yes":
		cookieSecure = true
	case "false", "0", "no":
		cookieSecure = false
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	// cancelOnErr holds the cancel func until the App takes ownership at successful return.
	// Any early-error return will trigger this defer, preventing a context leak.
	cancelOnErr := appCancel
	defer func() {
		if cancelOnErr != nil {
			cancelOnErr()
		}
	}()

	// ── Database (config-driven) ──────────────────────────────────────────
	var db dbconn.DBConn

	pg, err := repopg.New(repopg.Params{Config: cfg.ConfigSvc})
	if err == nil {
		db = pg
	} else {
		zlog.Warn().Err(err).Msg("postgres: disabled or failed to initialize, falling back to sqlite")
		sq, err := reposqlite.New(reposqlite.Params{Config: cfg.ConfigSvc})
		if err != nil {
			return nil, fmt.Errorf("sqlite: %w", err)
		}
		db = sq
	}

	if db == nil {
		return nil, errors.New("neither postgres nor sqlite must be enabled in config")
	}

	// Drop old single-column unique index on push_subscriptions.endpoint — replaced
	// by composite uniqueIndex(endpoint, workspace_id) to allow one subscription per
	// browser per workspace.
	_ = db.Conn(context.Background()).Exec("DROP INDEX IF EXISTS uni_push_subscriptions_endpoint").Error

	// idx_events_name_user_id / idx_workflows_name_user_id were created over
	// `name` alone despite their names, because only the Name field carried
	// the uniqueIndex tag. That made event and workflow names unique across
	// every account rather than per user. AutoMigrate will not redefine an
	// index that already exists, so the stale ones are dropped here and
	// recreated as the composite the model now declares. Dropping is safe:
	// the new index is strictly looser, so any data valid under the old one
	// is valid under the new one.
	_ = db.Conn(context.Background()).Exec("DROP INDEX IF EXISTS idx_events_name_user_id").Error
	_ = db.Conn(context.Background()).Exec("DROP INDEX IF EXISTS idx_workflows_name_user_id").Error

	if err := db.Conn(context.Background()).AutoMigrate(
		&model.Workspace{},
		&model.Task{},
		&model.Message{},
		&model.Telemetry{},
		&model.MCPClient{},
		&model.User{},
		&model.SlackWorkspaceLink{},
		&model.SlackTaskThread{},
		&model.PushSubscription{},
		&model.Event{},
		&model.EventTrigger{},
		&model.Workflow{},
		&model.WorkflowStep{},
		&model.ToolCall{},
	); err != nil {
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	// ── Core Services ──────────────────────────────────────────────────────────
	ids, err := idgen.New(uint16(1))
	if err != nil {
		return nil, fmt.Errorf("idgen: %w", err)
	}
	repo := base.New(db)
	bus := eventbus.New()

	storageSvc, err := storage.New("./_storage")
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}

	cfg.Storage.StorageDir = "./_storage"
	cleanupSvc, err := cleanup.New(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("cleanup: %w", err)
	}
	cleanupSvc.Start(appCtx)

	imgSvc := image.New()

	mqSvc, err := memq.New(memq.Params{})
	if err != nil {
		return nil, fmt.Errorf("memq: %w", err)
	}
	smtpSvc := smtp.New(cfg.SMTP)
	slackSvc := slacksvc.New(cfg.Slack)
	zlog.Info().
		Bool("configured_enabled", cfg.Slack.Enabled).
		Bool("is_enabled", slackSvc.IsEnabled()).
		Str("client_id", cfg.Slack.ClientID).
		Msg("[slack] integration startup status")

	pubsubSvc, err := pubsub.New(pubsub.Params{
		Config: cfg.ConfigSvc,
		IDGen:  ids,
	})
	if err != nil {
		return nil, fmt.Errorf("pubsub: %w", err)
	}

	// Ensure global topics exist
	if _, err := pubsubSvc.Create(context.Background(), pubsub.CreatePubSubRequest{ID: entity.PubSubTopicCRUD}); err != nil {
		return nil, fmt.Errorf("create global pubsub (id:%d): %w", entity.PubSubTopicCRUD, err)
	}
	if _, err := pubsubSvc.Create(context.Background(), pubsub.CreatePubSubRequest{ID: entity.PubSubTopicMCP}); err != nil {
		return nil, fmt.Errorf("create global pubsub (id:%d): %w", entity.PubSubTopicMCP, err)
	}
	if _, err := pubsubSvc.Create(context.Background(), pubsub.CreatePubSubRequest{ID: entity.PubSubTopicEvents}); err != nil {
		return nil, fmt.Errorf("create global pubsub (id:%d): %w", entity.PubSubTopicEvents, err)
	}

	// ── Controllers ────────────────────────────────────────────────────────────
	telemetryCtrl := telemetry.New(telemetry.Params{
		DB:        db,
		PubSub:    pubsubSvc,
		BatchSize: 1000,
		Interval:  5 * time.Second,
	})
	if err := telemetryCtrl.Start(appCtx); err != nil {
		zlog.Error().Err(err).Msg("failed to start telemetry controller")
	}

	tokenSvc := auth.NewTokenService(auth.TokenConfig{
		JWTSecret: cfg.Auth.JWTSecret,
	})

	rateLimiter := svclimit.New()

	crudCtrl := crud.New(crud.Params{
		IDGen:      ids,
		Repository: repo,
		Storage:    storageSvc,
		Image:      imgSvc,
		PubSub:     pubsubSvc,
		Limiter:    rateLimiter,
	})

	// ── Pub/Stats ─────────────────────────────────────────────────────────────
	pubStatsCtrl := pub.NewStatsController(pub.Params{
		Repository: repo,
		PubSub:     pubsubSvc,
	})
	if err := pubStatsCtrl.Start(context.Background()); err != nil {
		zlog.Error().Err(err).Msg("failed to start pub stats controller")
	}

	// ── Scheduler ─────────────────────────────────────────────────────────────
	schedSvc := scheduler.New(repo, ids, bus, pubsubSvc)
	schedSvc.Start(context.Background())

	// ── Auth ──────────────────────────────────────────────────────────
	authSvc := auth.NewGoogle(cfg.Auth.Google.ClientID, cfg.Auth.Google.ClientSecret, fmt.Sprintf("%s/api/v1/auth/google/callback", cfg.App.BaseURL))
	githubAuthSvc := auth.NewGitHub(cfg.Auth.GitHub.ClientID, cfg.Auth.GitHub.ClientSecret, fmt.Sprintf("%s/api/v1/auth/github/callback", cfg.App.BaseURL))

	// ── MCP manager ───────────────────────────────────────────────────────────
	mcpManager := mcp.NewManager(func(workspaceID int64, userID string) *mcp.WorkspaceServer {
		var workspaceOwner string
		workspace, err := repo.SystemGetWorkspace(context.Background(), workspaceID)
		if err == nil {
			workspaceOwner = monoflake.ID(workspace.UserID).String()
		} else {
			workspaceOwner = userID
		}

		srv := mcp.NewWorkspaceServer(
			workspaceID,
			workspaceOwner,
			cfg.App.BaseURL,
			func(ctx context.Context, task model.Task) (model.Task, error) {
				task.AllowAllCommands = workspace.AllowAllCommands
				res, err := repo.CreateTask(ctx, task)
				if err == nil {
					uid := monoflake.IDFromBase62(workspaceOwner).Int64()
					pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
						PubSubID: entity.PubSubTopicCRUD,
						Event: entity.CRUDEvent{
							Action:       entity.ActionTaskCreate,
							WorkspaceID:  workspaceID,
							UserID:       uid,
							ResourceType: entity.ResourceTask,
							ResourceID:   res.ID,
							Actor:        entity.ActorAgent,
							Origin:       entity.OriginMCP,
						},
					})
					bus.Publish(workspaceID, workspaceOwner, eventbus.Event{
						Type:    "task.created",
						Payload: mapper.FromModelTaskToView(res),
					})
				}
				return res, err
			},
			func(ctx context.Context, taskID int64, status string) (model.Task, error) {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				m, err := repo.GetTask(ctx, workspaceID, taskID, uid)
				if err != nil {
					return model.Task{}, err
				}
				if m.Status == status {
					return m, nil
				}
				m.Status = status

				msgID := ids.NextID()
				_ = repo.CreateMessage(ctx, model.Message{
					ID:        msgID,
					CreatedAt: time.Now(),
					TaskID:    taskID,
					UserID:    monoflake.IDFromBase62(workspaceOwner).Int64(),
					Sender:    "agent",
					Text:      fmt.Sprintf("Status updated to: %s", status),
				})

				updated, err := repo.UpdateTask(ctx, m)
				if err == nil {
					if updated.Status == "completed" || updated.Status == "done" {
						pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
							PubSubID: entity.PubSubTopicCRUD,
							Event: entity.CRUDEvent{
								Action:       entity.ActionTaskComplete,
								WorkspaceID:  workspaceID,
								UserID:       uid,
								ResourceType: entity.ResourceTask,
								ResourceID:   updated.ID,
								Actor:        entity.ActorAgent,
								Origin:       entity.OriginMCP,
							},
						})

					}
					pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
						PubSubID: entity.PubSubTopicCRUD,
						Event: entity.CRUDEvent{
							Action:       entity.ActionTaskUpdate,
							WorkspaceID:  workspaceID,
							UserID:       uid,
							ResourceType: entity.ResourceTask,
							ResourceID:   updated.ID,
							Actor:        entity.ActorAgent,
							Origin:       entity.OriginMCP,
						},
					})
					// Emit message event for the status update text
					pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
						PubSubID: entity.PubSubTopicCRUD,
						Event: entity.CRUDEvent{
							Action:       entity.ActionMessageCreate,
							WorkspaceID:  workspaceID,
							UserID:       uid,
							ResourceType: entity.ResourceMessage,
							ResourceID:   msgID,
							Actor:        entity.ActorAgent,
							Origin:       entity.OriginMCP,
						},
					})
				}
				return updated, err
			},
			func(ctx context.Context, taskID int64) (model.Task, error) {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				return repo.GetTask(ctx, workspaceID, taskID, uid)
			},
			func(ctx context.Context, filter mcp.ListTasksFilter) ([]model.Task, error) {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				return repo.ListTasks(ctx, entity.ListTasksRequest{
					WorkspaceID: workspaceID,
					UserID:      workspaceOwner,
					Status:      filter.Status,
					Limit:       filter.Limit,
				}, uid)
			},
			func(ctx context.Context) (model.Task, error) {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				return repo.GetNextTask(ctx, workspaceID, uid)
			},
			func(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error) {
				id := monoflake.IDFromBase62(chatID)
				if id == 0 {
					return 0, fmt.Errorf("invalid chat ID: %s", chatID)
				}
				taskID := id.Int64()

				for i := range attachments {
					if attachments[i].Data != "" {
						attachments[i].ID = monoflake.ID(ids.NextID()).String()
						_ = storageSvc.Save(attachments[i].ID, attachments[i].Data)
						attachments[i].Data = ""
					}
				}

				var attsData []byte
				if len(attachments) > 0 {
					attsData, _ = json.Marshal(attachments)
				}

				var metadataJSON datatypes.JSON
				if metadata != nil {
					if b, err := json.Marshal(metadata); err == nil {
						metadataJSON = datatypes.JSON(b)
					}
				}

				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				m, err := repo.GetTask(ctx, workspaceID, taskID, uid)
				if err == nil && m.Status == "notstarted" {
					m.Status = "ongoing"
					_, _ = repo.UpdateTask(ctx, m)
					_ = repo.CreateMessage(ctx, model.Message{
						ID:        ids.NextID(),
						CreatedAt: time.Now(),
						TaskID:    taskID,
						UserID:    uid,
						Sender:    "agent",
						Text:      "Status updated to: ongoing",
					})
				}

				msgID := ids.NextID()
				msg := model.Message{
					ID:          msgID,
					CreatedAt:   time.Now(),
					TaskID:      taskID,
					UserID:      uid,
					Sender:      "agent",
					Text:        text,
					Attachments: datatypes.JSON(attsData),
					Metadata:    metadataJSON,
				}

				if err := repo.CreateMessage(ctx, msg); err != nil {
					return 0, err
				}

				pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
					PubSubID: entity.PubSubTopicCRUD,
					Event: entity.CRUDEvent{
						Action:       entity.ActionMessageCreate,
						WorkspaceID:  workspaceID,
						UserID:       uid,
						ResourceType: entity.ResourceMessage,
						ResourceID:   msgID,
						Actor:        entity.ActorAgent,
						Origin:       entity.OriginMCP,
					},
				})

				uid = monoflake.IDFromBase62(workspaceOwner).Int64()
				latest, err := repo.GetTask(ctx, workspaceID, taskID, uid)
				if err == nil {
					bus.Publish(workspaceID, workspaceOwner, eventbus.Event{
						Type:    "task.updated",
						Payload: mapper.FromModelTaskToView(latest),
					})
				}
				return msgID, nil
			},
			func(ctx context.Context, taskID int64, messageID int64, metadata any) error {
				existingMetadata := make(map[string]any)
				if m, err := repo.SystemGetMessage(ctx, messageID); err == nil && len(m.Metadata) > 0 {
					_ = json.Unmarshal(m.Metadata, &existingMetadata)
				}

				newMetaBytes, _ := json.Marshal(metadata)
				newMetaMap := make(map[string]any)
				_ = json.Unmarshal(newMetaBytes, &newMetaMap)

				for k, v := range newMetaMap {
					existingMetadata[k] = v
				}

				b, _ := json.Marshal(existingMetadata)
				err := repo.UpdateMessageMetadata(ctx, taskID, messageID, b)
				if err == nil {
					uid := monoflake.IDFromBase62(workspaceOwner).Int64()
					latest, _ := repo.GetTask(ctx, workspaceID, taskID, uid)
					bus.Publish(workspaceID, workspaceOwner, eventbus.Event{
						Type:    "task.updated",
						Payload: mapper.FromModelTaskToView(latest),
					})

					// Publish ActionMessageUpdate event to pubsub so Slack and telemetry pick it up
					pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
						PubSubID: entity.PubSubTopicCRUD,
						Event: entity.CRUDEvent{
							Action:       entity.ActionMessageUpdate,
							WorkspaceID:  workspaceID,
							UserID:       uid,
							ResourceType: entity.ResourceMessage,
							ResourceID:   messageID,
							Actor:        entity.ActorHuman,
							Origin:       entity.OriginAPI,
						},
					})
				}
				return err
			},
			func(ctx context.Context, tools []string) error {
				return crudCtrl.UpdateWorkspaceAutoAllowedTools(ctx, entity.UpdateWorkspaceAutoAllowedToolsRequest{
					WorkspaceID: workspace.ID,
					Tools:       tools,
					UserID:      monoflake.ID(workspace.UserID).String(),
				})
			},
			func(ctx context.Context, eventName string, payload string, faq []entity.EventFAQ, run mcp.WorkflowRunContext) error {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				ev, err := repo.GetEventByName(ctx, eventName, uid)
				if err != nil {
					return fmt.Errorf("event %q not found", eventName)
				}

				// The run comes from the publishing task and nowhere else: the
				// agent identifies its task, and the task row carries both the
				// workflow and the hop count. A workflow is never named by the
				// caller, so there is no second source to reconcile against.
				pubsubSvc.Publish(ctx, pubsub.PublishRequest{
					PubSubID: entity.PubSubTopicEvents,
					Event: entity.EventPublishedPayload{
						EventID:    ev.ID,
						Name:       ev.Name,
						Payload:    payload,
						FAQ:        faq,
						WorkflowID: run.WorkflowID,
						Depth:      run.Depth,
					},
				})
				return nil
			},
			func(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
				created, err := repo.CreateToolCall(ctx, tc)
				if err == nil {
					uid := monoflake.IDFromBase62(workspaceOwner).Int64()
					if latest, gErr := repo.GetTask(ctx, workspaceID, created.TaskID, uid); gErr == nil {
						bus.Publish(workspaceID, workspaceOwner, eventbus.Event{
							Type:    "task.updated",
							Payload: mapper.FromModelTaskToView(latest),
						})
					}
				}
				return created, err
			},
			func(ctx context.Context, id int64, status string) error {
				updated, err := repo.UpdateToolCallStatus(ctx, id, status)
				if err != nil {
					return err
				}
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				if latest, gErr := repo.GetTask(ctx, workspaceID, updated.TaskID, uid); gErr == nil {
					bus.Publish(workspaceID, workspaceOwner, eventbus.Event{
						Type:    "task.updated",
						Payload: mapper.FromModelTaskToView(latest),
					})
				}
				return nil
			},
			bus,
			ids,
			storageSvc,
			workspace.Icon,
			workspace.Name,
			workspace.Description,
			workspace.ArchivedAt,
			func() []string {
				var tools []string
				if len(workspace.AutoAllowedTools) > 0 {
					_ = json.Unmarshal(workspace.AutoAllowedTools, &tools)
				}
				return tools
			}(),
			tokenSvc,
			pubsubSvc,
		)
		srv.StartPoller(repo)
		srv.StartPing()
		return srv
	})

	slackCtrl := slackctrl.New(slackctrl.Params{
		Repository: repo,
		SlackSvc:   slackSvc,
		Crud:       crudCtrl,
		MCPManager: mcpManager,
		PubSub:     pubsubSvc,
		TokenSvc:   tokenSvc,
		TokenKey:   cfg.Auth.WorkspaceTokenKey,
		BaseURL:    cfg.App.BaseURL,
	})
	if err := slackCtrl.Start(context.Background()); err != nil {
		zlog.Error().Err(err).Msg("failed to start slack controller")
	}

	notificationSvc, err := notification.New(notification.Params{
		Repository: repo,
		PubSub:     pubsubSvc,
		MemQ:       mqSvc,
		SMTP:       smtpSvc,
		BaseURL:    cfg.App.BaseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("notification: %w", err)
	}
	if err := notificationSvc.Start(context.Background()); err != nil {
		zlog.Error().Err(err).Msg("failed to start notification controller")
	}

	pushCtrl := pushctrl.New(pushctrl.Params{
		Config:     cfg.WebPush,
		Repository: repo,
		PubSub:     pubsubSvc,
		IDGen:      ids,
	})
	if err := pushCtrl.Start(context.Background()); err != nil {
		zlog.Error().Err(err).Msg("failed to start push controller")
	}

	eventConsumer := eventctrl.New(eventctrl.Params{
		Repository: repo,
		PubSub:     pubsubSvc,
		IDGen:      ids,
		Bus:        bus,
	})
	if err := eventConsumer.Start(context.Background()); err != nil {
		zlog.Error().Err(err).Msg("failed to start event consumer")
	}

	// Central PubSub to EventBus SSE Forwarder
	go func() {
		sub, err := pubsubSvc.Subscribe(context.Background(), pubsub.SubscribeRequest{
			PubSubID: entity.PubSubTopicCRUD,
		})
		if err != nil {
			zlog.Error().Err(err).Msg("[app] failed to subscribe to CRUD topic for SSE forwarding")
			return
		}
		zlog.Info().Msg("[app] started central SSE event forwarder")
		for eventMsg := range sub.Events {
			event, ok := eventMsg.(entity.CRUDEvent)
			if !ok {
				continue
			}

			// Forward task and message updates to the EventBus in the background
			go func(ev entity.CRUDEvent) {
				ctx := context.Background()
				ws, err := repo.SystemGetWorkspace(ctx, ev.WorkspaceID)
				if err != nil {
					return
				}
				ownerID := monoflake.ID(ws.UserID).String()

				var taskID int64
				var eventType string

				switch ev.ResourceType {
				case entity.ResourceTask:
					taskID = ev.ResourceID
					switch ev.Action {
					case entity.ActionTaskCreate:
						eventType = "task.created"
					case entity.ActionTaskComplete:
						eventType = "status.updated"
					default:
						eventType = "task.updated"
					}
				case entity.ResourceMessage:
					if ev.Action == entity.ActionMessageCreate {
						msg, err := repo.SystemGetMessage(ctx, ev.ResourceID)
						if err == nil {
							taskID = msg.TaskID
							eventType = "reply.received"
						}
					}
				}

				if taskID != 0 && eventType != "" {
					t, err := taskEventPayload(ctx, repo, taskID)
					if err == nil {
						bus.Publish(ev.WorkspaceID, ownerID, eventbus.Event{
							Type:    eventType,
							Payload: mapper.FromModelTaskToView(t),
						})
					}
				}
			}(event)
		}
		zlog.Warn().Msg("[app] central SSE forwarder pubsub channel closed")
	}()

	// ── Fiber & Routing ────────────────────────────────────────────────────────
	fiberApp := fiber.New(fiber.Config{
		DisableStartupMessage: false,
		BodyLimit:             4 * 1024 * 1024, // 4 MB
	})
	fiberApp.Use(cors.New(cors.Config{
		AllowOrigins:  "*",
		AllowHeaders:  "Origin, Content-Type, Accept, mcp-session-id, mcp-protocol-version",
		ExposeHeaders: "mcp-session-id, mcp-protocol-version",
	}))
	fiberApp.Use(fiberzerolog.New(fiberzerolog.Config{
		Logger: &zlog.Logger,
		Fields: []string{
			"ip",
			"latency",
			"method",
			"path",
			"error",
			"status",
		},
		Next: func(c *fiber.Ctx) bool {
			return c.Method() == fiber.MethodOptions
		},
	}))

	// Static Assets
	fiberApp.Static("/", "./public", fiber.Static{
		Compress: false,
		Next: func(c *fiber.Ctx) bool {
			return skipStaticHandler(c.Path())
		},
	})

	mux := http.NewServeMux()

	// Slack Webhook Handler
	handlerslack.New(handlerslack.Params{
		SlackCtrl: slackCtrl,
		SlackSvc:  slackSvc,
		TokenSvc:  tokenSvc,
		BaseURL:   cfg.App.BaseURL,
		Mux:       mux,
	})

	// CoreMCP Handler
	if _, err := handlercoremcp.New(handlercoremcp.Params{
		Crud:     crudCtrl,
		TokenSvc: tokenSvc,
		BaseURL:  cfg.App.BaseURL,
		Domain:   cfg.App.Domain,
		Mux:      mux,
	}); err != nil {
		return nil, fmt.Errorf("coremcp handler: %w", err)
	}

	// MCP Handler
	if _, err := handlermcp.New(handlermcp.Params{
		MCPManager: mcpManager,
		Crud:       crudCtrl,
		TokenSvc:   tokenSvc,
		BaseURL:    cfg.App.BaseURL,
		Mux:        mux,
	}); err != nil {
		return nil, fmt.Errorf("mcp handler: %w", err)
	}

	// API Handler
	apiGroup := fiberApp.Group("/api/v1")
	if _, err := handlerapi.New(handlerapi.Params{
		Crud:             crudCtrl,
		Auth:             authSvc,
		GithubAuth:       githubAuthSvc,
		TokenSvc:         tokenSvc,
		MCPManager:       mcpManager,
		EventBus:         bus,
		BaseURL:          cfg.App.BaseURL,
		MCPBaseURL:       cfg.App.BaseURL,
		Domain:           cfg.App.Domain,
		CookieSecure:     cookieSecure,
		BasePath:         cfg.App.BasePath,
		RootLoginEnabled: cfg.Auth.RootLoginEnabled,
		RootToken:        cfg.Auth.RootAccessToken,
		GithubClientID:   cfg.Auth.GitHub.ClientID,
		Router:           apiGroup,
		SlackCtrl:        slackCtrl,
		PushCtrl:         pushCtrl,
	}); err != nil {
		return nil, fmt.Errorf("api handler: %w", err)
	}

	// SPA Fallback
	fiberApp.Get("/*", func(c *fiber.Ctx) error {
		path := c.Path()
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/mcp") || strings.HasPrefix(path, "/.well-known/") {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not Found"})
		}
		// A missing static asset is a genuine 404 rather than the app shell:
		// handing index.html back to a <script src> only turns a missing file
		// into a syntax error further down the line.
		if namesAFile(path) && !strings.HasSuffix(path, ".html") {
			return c.Status(fiber.StatusNotFound).SendString("Not Found")
		}
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Set("Pragma", "no-cache")
		c.Set("Expires", "0")

		htmlBytes, err := os.ReadFile("./public/index.html")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("index.html not found")
		}

		htmlContent := string(htmlBytes)

		// Fix relative asset paths generated by Vite (base: '') for nested SPA routes
		baseHref := cfg.App.BasePath + "/"
		htmlContent = strings.ReplaceAll(htmlContent, `src="./`, `src="`+baseHref)
		htmlContent = strings.ReplaceAll(htmlContent, `href="./`, `href="`+baseHref)

		injectScript := fmt.Sprintf(`<script>
			window.__AGENTRQ_BASE_PATH__ = %q;
			if (window.__AGENTRQ_BASE_PATH__ && window.location.pathname === window.__AGENTRQ_BASE_PATH__) {
				window.location.replace(window.__AGENTRQ_BASE_PATH__ + '/');
			}
		</script>`, cfg.App.BasePath)

		// Search case-insensitively for the head tag
		headIndex := strings.Index(strings.ToLower(htmlContent), "<head>")
		if headIndex != -1 {
			htmlContent = htmlContent[:headIndex+6] + injectScript + htmlContent[headIndex+6:]
		} else {
			// Fallback: search for <head
			headTagIndex := strings.Index(strings.ToLower(htmlContent), "<head")
			if headTagIndex != -1 {
				// Find closing bracket
				closeBracket := strings.Index(htmlContent[headTagIndex:], ">")
				if closeBracket != -1 {
					insertPos := headTagIndex + closeBracket + 1
					htmlContent = htmlContent[:insertPos] + injectScript + htmlContent[insertPos:]
				}
			}
		}

		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.SendString(htmlContent)
	})

	// ── Server Start ─────────────────────────────────────────────────
	mux.Handle("/pub/stats", pubStatsHandler(pubStatsCtrl))
	mux.Handle("/api/v1/workspaces/{id}/events", eventsHandler(crudCtrl, bus, tokenSvc))
	mux.Handle("/api/v1/events/stream", eventsHandler(crudCtrl, bus, tokenSvc))
	mux.Handle("/", adaptor.FiberApp(fiberApp))

	var finalRouter http.Handler = mux
	finalRouter = ratelimit.New(cfg.Ratelimit.Enabled, cfg.Ratelimit.MaxPerIP, cfg.Ratelimit.MaxPerUser, cfg.Ratelimit.Window, tokenSvc)(finalRouter)
	finalRouter = ddos.New(cfg.Ddos.Enabled, cfg.Ddos.MaxRequestsPerSecond, cfg.Ddos.BlockDuration)(finalRouter)

	serverSvc, err := server.New(server.Params{
		Config: server.Config{
			Port:               cfg.App.Port,
			SSLPort:            cfg.App.SSLPort,
			SSLEnabled:         cfg.SSL.Enabled,
			Domain:             cfg.App.Domain,
			ProxyDomain:        cfg.App.ProxyDomain,
			SSLCacheDir:        cfg.SSL.CacheDir,
			LetsencryptEmail:   cfg.SSL.LetsencryptEmail,
			CloudflareAPIToken: cfg.SSL.CloudflareAPIToken,
		},
		Router: finalRouter,
	})
	if err != nil {
		return nil, fmt.Errorf("server service: %w", err)
	}

	cancelOnErr = nil // App takes ownership; defer must not cancel.
	return &App{server: serverSvc, bus: bus, pubsub: pubsubSvc, telemetry: telemetryCtrl, cancel: appCancel}, nil
}

// taskEventPayload loads the task an SSE event is about, with the relations a
// task view carries.
//
// A client replaces its whole task with this payload, so a relation missing
// from it is a relation the client loses: publishing a task loaded without its
// tool calls is what used to empty the trajectory's tool lane every time a new
// message arrived. SystemGetTask preloads nothing — it is the system-scoped
// lookup, used in plenty of places that want only the row — so the relations
// are loaded here rather than widened for every caller.
//
// A relation that fails to load is left as it is instead of failing the
// publish: an event that reports a status change late-but-complete is worth
// more than no event at all.
func taskEventPayload(ctx context.Context, repo base.Repository, taskID int64) (model.Task, error) {
	t, err := repo.SystemGetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if messages, err := repo.ListMessages(ctx, t.ID); err == nil {
		t.Messages = messages
	}
	if toolCalls, err := repo.ListToolCalls(ctx, t.ID); err == nil {
		t.ToolCalls = toolCalls
	}
	return t, nil
}

// namesAFile reports whether a request path names a file rather than a page.
//
// A file carries an extension in its last segment; an SPA route — /login, or
// /workspaces/abc — does not. The frontend build has no extensionless output,
// so the distinction holds for everything actually on disk.
func namesAFile(path string) bool {
	dot := strings.LastIndex(path, ".")
	return dot != -1 && dot > strings.LastIndex(path, "/")
}

// skipStaticHandler reports whether a request should bypass the static file
// handler and go straight to the routes behind it.
//
// SPA routes are skipped because there is no file to find: the static handler
// would open a path that was never going to exist, and it logs every such miss
// before passing the request on. That log is not suppressed here — the app is
// mounted into the stdlib mux through an adaptor, whose synthetic request
// context has no server carrying Fiber's silent logger — so each page a user
// navigated to wrote an error line for a request that then succeeded.
//
// A path that does name a file is still handed over, so a genuinely missing
// asset is still reported.
func skipStaticHandler(path string) bool {
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/mcp") {
		return true
	}
	if path == "/" || path == "/index.html" {
		return true
	}
	return !namesAFile(path)
}

func pubStatsHandler(ctrl pub.StatsController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "https://agentrq.com")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		ch := ctrl.Subscribe()
		defer ctrl.Unsubscribe(ch)

		for {
			select {
			case data, ok := <-ch:
				if !ok {
					return
				}
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(data)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func eventsHandler(ctrl crud.Controller, bus *eventbus.Bus, tokenSvc auth.TokenService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("at")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := tokenSvc.ValidateToken(cookie.Value)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := claims.Subject

		workspaceIDParam := r.PathValue("id")
		var workspaceID int64
		if workspaceIDParam != "" {
			workspaceID = monoflake.IDFromBase62(workspaceIDParam).Int64()
			if workspaceID == 0 {
				http.Error(w, "Invalid workspace ID", http.StatusUnprocessableEntity)
				return
			}

			if ok, err := ctrl.CheckWorkspaceAccess(r.Context(), workspaceID, userID); err != nil || !ok {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := bus.Subscribe(workspaceID, userID)
		defer bus.Unsubscribe(workspaceID, userID, ch)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Commit the header block right away. net/http buffers the response
		// until something is written or flushed, so without this the client
		// receives nothing at all until the first event or the keepalive tick
		// below — up to 30 seconds. EventSource only fires `onopen` once the
		// headers land, which left the UI reporting a healthy stream as
		// disconnected for that whole window.
		flusher.Flush()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case data, ok := <-ch:
				if !ok {
					return
				}
				_, _ = w.Write(data)
				flusher.Flush()
			case <-ticker.C:
				_, _ = w.Write([]byte(": agentrq\n\n"))
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func (a *App) Run() error {
	defer a.cancel()
	return a.server.Run()
}

// Shutdown gracefully stops the app: it cancels the app context (signaling background
// consumers wired to it to stop), flushes buffered telemetry so nothing is lost, and
// shuts the HTTP server down within ctx's deadline. Safe to call once on SIGINT/SIGTERM.
func (a *App) Shutdown(ctx context.Context) error {
	a.cancel()
	if a.telemetry != nil {
		a.telemetry.Close()
	}
	return a.server.Shutdown(ctx)
}
