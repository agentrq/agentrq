// Package app wires all dependencies and starts the server.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	"gorm.io/datatypes"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/controller/notification"
	"github.com/agentrq/agentrq/backend/internal/controller/telemetry"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	handlerapi "github.com/agentrq/agentrq/backend/internal/handler/api"
	handlermcp "github.com/agentrq/agentrq/backend/internal/handler/mcp"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	repopg "github.com/agentrq/agentrq/backend/internal/repository/postgres"
	reposqlite "github.com/agentrq/agentrq/backend/internal/repository/sqlite"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/scheduler"
	"github.com/agentrq/agentrq/backend/internal/service/server"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/mustafaturan/monoflake"
)

type (
	Config struct {
		App struct {
			Port    int    `yaml:"port"`
			SSLPort int    `yaml:"sslPort"`
			BaseURL string `yaml:"baseUrl"`
			Domain  string `yaml:"domain"` // e.g. agentrq.com
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
			JWTSecret         string `yaml:"jwtSecret"`
			RootAccessToken   string `yaml:"rootAccessToken"`
			RootLoginEnabled  bool   `yaml:"rootLoginEnabled"`
			WorkspaceTokenKey string `yaml:"workspaceTokenKey"`
		} `yaml:"auth"`
		SMTP      smtp.Config    `yaml:"smtp"`
		ConfigSvc config.Service `yaml:"-"` // injected, not from YAML
	}

	App struct {
		server server.Service
		bus    *eventbus.Bus
		pubsub pubsub.Service
	}
)

func New(cfg Config) (*App, error) {
	cfg.App.BaseURL = strings.TrimSuffix(cfg.App.BaseURL, "/")
	if cfg.App.BaseURL == "" {
		cfg.App.BaseURL = fmt.Sprintf("http://localhost:%d", cfg.App.Port)
	}

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

	if err := db.Conn(context.Background()).AutoMigrate(&model.Workspace{}, &model.Task{}, &model.Message{}, &model.Telemetry{}, &model.User{}); err != nil {
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
	imgSvc := image.New()

	mqSvc, err := memq.New(memq.Params{})
	if err != nil {
		return nil, fmt.Errorf("memq: %w", err)
	}
	smtpSvc := smtp.New(cfg.SMTP)

	pubsubSvc, err := pubsub.New(pubsub.Params{
		Config: cfg.ConfigSvc,
		IDGen:  ids,
	})
	if err != nil {
		return nil, fmt.Errorf("pubsub: %w", err)
	}

	// Ensure global topics (0 and 2) exist
	if _, err := pubsubSvc.Create(context.Background(), pubsub.CreatePubSubRequest{ID: 0}); err != nil {
		return nil, fmt.Errorf("create global pubsub (id:0): %w", err)
	}
	if _, err := pubsubSvc.Create(context.Background(), pubsub.CreatePubSubRequest{ID: 2}); err != nil {
		return nil, fmt.Errorf("create global pubsub (id:2): %w", err)
	}

	// ── Controllers ────────────────────────────────────────────────────────────
	telemetryCtrl := telemetry.New(telemetry.Params{
		DB:        db,
		PubSub:    pubsubSvc,
		BatchSize: 1000,
		Interval:  5 * time.Second,
	})
	if err := telemetryCtrl.Start(context.Background()); err != nil {
		zlog.Error().Err(err).Msg("failed to start telemetry controller")
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
	notificationSvc.Start(context.Background())

	tokenSvc := auth.NewTokenService(auth.TokenConfig{
		JWTSecret: cfg.Auth.JWTSecret,
	})

	crudCtrl := crud.New(crud.Params{
		IDGen:      ids,
		Repository: repo,
		Storage:    storageSvc,
		Image:      imgSvc,
		PubSub:     pubsubSvc,
		TokenKey:   cfg.Auth.WorkspaceTokenKey,
	})

	// ── Scheduler ─────────────────────────────────────────────────────────────
	schedSvc := scheduler.New(repo, ids, bus, pubsubSvc)
	schedSvc.Start(context.Background())

	// ── Auth ──────────────────────────────────────────────────────────
	authSvc := auth.New(cfg.Auth.Google.ClientID, cfg.Auth.Google.ClientSecret, fmt.Sprintf("%s/api/v1/auth/google/callback", cfg.App.BaseURL))

	// ── MCP manager ───────────────────────────────────────────────────────────
	mcpManager := mcpctrl.NewManager(func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer {
		var workspaceOwner string
		workspace, err := repo.SystemGetWorkspace(context.Background(), workspaceID)
		if err == nil {
			workspaceOwner = monoflake.ID(workspace.UserID).String()
		} else {
			workspaceOwner = userID
		}

		srv := mcpctrl.NewWorkspaceServer(
			workspaceID,
			workspaceOwner,
			cfg.App.BaseURL,
			func(ctx context.Context, task model.Task) (model.Task, error) {
				res, err := repo.CreateTask(ctx, task)
				if err == nil {
					uid := monoflake.IDFromBase62(workspaceOwner).Int64()
					existingTasks, _ := repo.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: workspaceID}, uid)
					hasOngoing := false
					for _, t := range existingTasks {
						if t.Status == "ongoing" && t.ID != res.ID {
							hasOngoing = true
							break
						}
					}
					if !hasOngoing {
						pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
							PubSubID: 0,
							Event: entity.CRUDEvent{
								Action:       entity.ActionTaskCreate,
								WorkspaceID:  workspaceID,
								UserID:       uid,
								ResourceType: entity.ResourceTask,
								ResourceID:   res.ID,
								Actor:        entity.ActorAgent,
							},
						})
					}
					bus.Publish(workspaceID, eventbus.Event{
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

				_ = repo.CreateMessage(ctx, model.Message{
					ID:        ids.NextID(),
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
							PubSubID: 0,
							Event: entity.CRUDEvent{
								Action:       entity.ActionTaskComplete,
								WorkspaceID:  workspaceID,
								UserID:       uid,
								ResourceType: entity.ResourceTask,
								ResourceID:   updated.ID,
								Actor:        entity.ActorAgent,
							},
						})
					}
					pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
						PubSubID: 0,
						Event: entity.CRUDEvent{
							Action:       entity.ActionTaskUpdate,
							WorkspaceID:  workspaceID,
							UserID:       uid,
							ResourceType: entity.ResourceTask,
							ResourceID:   updated.ID,
							Actor:        entity.ActorAgent,
						},
					})
				}
				return updated, err
			},
			func(ctx context.Context, taskID int64) (model.Task, error) {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				return repo.GetTask(ctx, workspaceID, taskID, uid)
			},
			func(ctx context.Context) ([]model.Task, error) {
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				return repo.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: workspaceID, UserID: workspaceOwner}, uid)
			},
			func(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error) {
				id := monoflake.IDFromBase62(chatID)
				if id == 0 {
					return 0, fmt.Errorf("invalid chat ID: %s", chatID)
				}
				taskID := id.Int64()

				for i := range attachments {
					if attachments[i].ID == "" {
						attachments[i].ID = monoflake.ID(ids.NextID()).String()
					}
					if attachments[i].Data != "" {
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

				isPermissionRequest := len(metadataJSON) > 0 && strings.Contains(string(metadataJSON), `"type":"permission_request"`)
				if isPermissionRequest {
					pubsubSvc.Publish(context.Background(), pubsub.PublishRequest{
						PubSubID: 0,
						Event: entity.CRUDEvent{
							Action:       entity.ActionMessageCreate,
							WorkspaceID:  workspaceID,
							UserID:       uid,
							ResourceType: entity.ResourceMessage,
							ResourceID:   msg.ID,
							Actor:        entity.ActorAgent,
						},
					})
				}

				uid = monoflake.IDFromBase62(workspaceOwner).Int64()
				latest, err := repo.GetTask(ctx, workspaceID, taskID, uid)
				if err == nil {
					bus.Publish(workspaceID, eventbus.Event{
						Type:    "task.updated",
						Payload: mapper.FromModelTaskToView(latest),
					})
				}
				return msgID, nil
			},
			func(ctx context.Context, taskID int64, messageID int64, metadata any) error {
				b, _ := json.Marshal(metadata)
				err := repo.UpdateMessageMetadata(ctx, messageID, b)
				if err == nil {
					uid := monoflake.IDFromBase62(workspaceOwner).Int64()
					latest, _ := repo.GetTask(ctx, workspaceID, taskID, uid)
					bus.Publish(workspaceID, eventbus.Event{
						Type:    "task.updated",
						Payload: mapper.FromModelTaskToView(latest),
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

	// ── Fiber & Routing ────────────────────────────────────────────────────────
	fiberApp := fiber.New(fiber.Config{
		DisableStartupMessage: false,
	})
	fiberApp.Use(cors.New(cors.Config{
		AllowOrigins:  "*",
		AllowHeaders:  "Origin, Content-Type, Accept, mcp-session-id, mcp-protocol-version",
		ExposeHeaders: "mcp-session-id, mcp-protocol-version",
	}))

	// Static Assets
	fiberApp.Static("/", "./public", fiber.Static{
		Compress: false,
		Next: func(c *fiber.Ctx) bool {
			return strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/mcp")
		},
	})

	// MCP Handler
	mux := http.NewServeMux()
	if _, err := handlermcp.New(handlermcp.Params{
		MCPManager: mcpManager,
		Repository: repo,
		TokenSvc:   tokenSvc,
		TokenKey:   cfg.Auth.WorkspaceTokenKey,
		Mux:        mux,
	}); err != nil {
		return nil, fmt.Errorf("mcp handler: %w", err)
	}

	// API Handler
	apiGroup := fiberApp.Group("/api/v1")
	if _, err := handlerapi.New(handlerapi.Params{
		Crud:             crudCtrl,
		Auth:             authSvc,
		TokenSvc:         tokenSvc,
		MCPManager:       mcpManager,
		EventBus:         bus,
		BaseURL:          cfg.App.BaseURL,
		MCPBaseURL:       cfg.App.BaseURL,
		Domain:           cfg.App.Domain,
		SSLEnabled:       cfg.SSL.Enabled,
		TokenKey:         cfg.Auth.WorkspaceTokenKey,
		RootLoginEnabled: cfg.Auth.RootLoginEnabled,
		RootToken:        cfg.Auth.RootAccessToken,
		Router:           apiGroup,
	}); err != nil {
		return nil, fmt.Errorf("api handler: %w", err)
	}

	// SPA Fallback
	fiberApp.Get("/*", func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/mcp") {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not Found"})
		}
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Set("Pragma", "no-cache")
		c.Set("Expires", "0")
		return c.SendFile("./public/index.html")
	})

	// ── Server Start ─────────────────────────────────────────────────
	mux.Handle("/api/v1/workspaces/{id}/events", eventsHandler(bus))
	mux.Handle("/api/v1/events", eventsHandler(bus))
	mux.Handle("/", adaptor.FiberApp(fiberApp))

	serverSvc, err := server.New(server.Params{
		Config: server.Config{
			Port:               cfg.App.Port,
			SSLPort:            cfg.App.SSLPort,
			SSLEnabled:         cfg.SSL.Enabled,
			Domain:             cfg.App.Domain,
			SSLCacheDir:        cfg.SSL.CacheDir,
			LetsencryptEmail:   cfg.SSL.LetsencryptEmail,
			CloudflareAPIToken: cfg.SSL.CloudflareAPIToken,
		},
		Router: mux,
	})
	if err != nil {
		return nil, fmt.Errorf("server service: %w", err)
	}

	return &App{server: serverSvc, bus: bus, pubsub: pubsubSvc}, nil
}

func eventsHandler(bus *eventbus.Bus) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceIDParam := r.PathValue("id")
		var workspaceID int64
		if workspaceIDParam != "" {
			workspaceID = monoflake.IDFromBase62(workspaceIDParam).Int64()
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := bus.Subscribe(workspaceID)
		defer bus.Unsubscribe(workspaceID, ch)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}
		for {
			select {
			case data, ok := <-ch:
				if !ok {
					return
				}
				_, _ = w.Write(data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
}

func (a *App) Run() error {
	return a.server.Run()
}
