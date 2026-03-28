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

	"gorm.io/datatypes"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/hasmcp/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/hasmcp/agentrq/backend/internal/controller/mcp"
	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	handlerapi "github.com/hasmcp/agentrq/backend/internal/handler/api"
	handlermcp "github.com/hasmcp/agentrq/backend/internal/handler/mcp"
	mapper "github.com/hasmcp/agentrq/backend/internal/mapper/api"
	"github.com/hasmcp/agentrq/backend/internal/repository/base"
	"github.com/hasmcp/agentrq/backend/internal/repository/dbconn"
	repopg "github.com/hasmcp/agentrq/backend/internal/repository/postgres"
	reposqlite "github.com/hasmcp/agentrq/backend/internal/repository/sqlite"
	"github.com/hasmcp/agentrq/backend/internal/service/auth"
	"github.com/hasmcp/agentrq/backend/internal/service/config"
	"github.com/hasmcp/agentrq/backend/internal/service/eventbus"
	"github.com/hasmcp/agentrq/backend/internal/service/idgen"
	"github.com/hasmcp/agentrq/backend/internal/service/image"
	"github.com/hasmcp/agentrq/backend/internal/service/memq"
	"github.com/hasmcp/agentrq/backend/internal/service/notif"
	"github.com/hasmcp/agentrq/backend/internal/service/scheduler"
	"github.com/hasmcp/agentrq/backend/internal/service/server"
	"github.com/hasmcp/agentrq/backend/internal/service/smtp"
	"github.com/hasmcp/agentrq/backend/internal/service/storage"
	"github.com/hasmcp/agentrq/backend/internal/service/telemetry"
	"github.com/mustafaturan/monoflake"
)

type Config struct {
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

type App struct {
	server server.Service
	bus    *eventbus.Bus
}

func New(cfg Config) (*App, error) {
	cfg.App.BaseURL = strings.TrimSuffix(cfg.App.BaseURL, "/")
	if cfg.App.BaseURL == "" {
		cfg.App.BaseURL = fmt.Sprintf("http://localhost:%d", cfg.App.Port)
	}

	// ── Database (config-driven: postgres → sqlite fallback) ──────────────────
	var db dbconn.DBConn

	pg, err := repopg.New(repopg.Params{Config: cfg.ConfigSvc})
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	db = pg

	if db == nil {
		sq, err := reposqlite.New(reposqlite.Params{Config: cfg.ConfigSvc})
		if err != nil {
			return nil, fmt.Errorf("sqlite: %w", err)
		}
		db = sq
	}

	if db == nil {
		return nil, errors.New("either postgres or sqlite must be enabled in config")
	}

	if err := db.Conn(context.Background()).AutoMigrate(&model.Workspace{}, &model.Task{}, &model.Message{}, &model.Telemetry{}, &model.User{}); err != nil {
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	// ── Services ──────────────────────────────────────────────────────────────
	ids, err := idgen.New(uint16(1))
	if err != nil {
		return nil, fmt.Errorf("idgen: %w", err)
	}
	repo := base.New(db)
	telemetrySvc := telemetry.New(db, repo)
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
	notifSvc, err := notif.New(repo, mqSvc, smtpSvc, cfg.App.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("notif: %w", err)
	}

	tokenSvc := auth.NewTokenService(auth.TokenConfig{
		JWTSecret: cfg.Auth.JWTSecret,
	})

	crudCtrl := crud.New(crud.Params{
		IDGen:      ids,
		Repository: repo,
		Storage:    storageSvc,
		Image:      imgSvc,
		Notif:      notifSvc,
		TokenKey:   cfg.Auth.WorkspaceTokenKey,
		Telemetry:  telemetrySvc,
	})

	// ── Scheduler ─────────────────────────────────────────────────────────────
	schedSvc := scheduler.New(repo, ids, bus, telemetrySvc)
	schedSvc.Start(context.Background())

	// ── Auth service ──────────────────────────────────────────────────────────
	authSvc := auth.New(cfg.Auth.Google.ClientID, cfg.Auth.Google.ClientSecret, fmt.Sprintf("%s/api/v1/auth/google/callback", cfg.App.BaseURL))

	// ── MCP manager ───────────────────────────────────────────────────────────
	mcpManager := mcpctrl.NewManager(func(workspaceID int64, userID string) *mcpctrl.WorkspaceServer {
		// When the MCP server starts, it needs to know who the workspace owner is
		// to correctly scope its repo calls.
		var workspaceOwner string
		workspace, err := repo.SystemGetWorkspace(context.Background(), workspaceID)
		if err == nil {
			workspaceOwner = monoflake.ID(workspace.UserID).String()
		} else {
			// Fallback to userID passed from the manager if getWorkspace fails
			workspaceOwner = userID
		}

		srv := mcpctrl.NewWorkspaceServer(
			workspaceID,
			workspaceOwner,
			cfg.App.BaseURL,
			func(ctx context.Context, task model.Task) (model.Task, error) {
				res, err := repo.CreateTask(ctx, task)
				if err == nil {
					// Only notify if there is no ongoing task — avoids spamming the human
					// while the agent is actively working.
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
						if w, err := repo.SystemGetWorkspace(ctx, workspaceID); err == nil {
							notifSvc.NotifyTaskCreated(w, res)
						}
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

				// Add message to chat about status change
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
						uid := monoflake.IDFromBase62(workspaceOwner).Int64()
						telemetrySvc.Record(ctx, uid, workspaceID, model.ActionIDTaskComplete)
					}
					if w, err := repo.SystemGetWorkspace(ctx, workspaceID); err == nil {
						notifSvc.NotifyTaskStatusUpdated(w, updated)
					}
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
				// The chatID is now expected to be the Base62 task ID
				id := monoflake.IDFromBase62(chatID)
				if id == 0 {
					return 0, fmt.Errorf("invalid chat ID: %s", chatID)
				}
				taskID := id.Int64()

				// Assign IDs to agent-provided attachments if missing and save binary
				for i := range attachments {
					if attachments[i].ID == "" {
						attachments[i].ID = monoflake.ID(ids.NextID()).String()
					}
					if attachments[i].Data != "" {
						_ = storageSvc.Save(attachments[i].ID, attachments[i].Data)
						attachments[i].Data = "" // clear for DB storage
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

				// If the task was not started, mark as ongoing when agent replies
				uid := monoflake.IDFromBase62(workspaceOwner).Int64()
				m, err := repo.GetTask(ctx, workspaceID, taskID, uid)
				if err == nil && m.Status == "notstarted" {
					m.Status = "ongoing"
					_, _ = repo.UpdateTask(ctx, m)
					// Also add a status message
					_ = repo.CreateMessage(ctx, model.Message{
						ID:        ids.NextID(),
						CreatedAt: time.Now(),
						TaskID:    taskID,
						UserID:    monoflake.IDFromBase62(workspaceOwner).Int64(),
						Sender:    "agent",
						Text:      "Status updated to: ongoing",
					})
				}

				// Create the main message AFTER any status messages so it always has
				// the latest timestamp — critical for the pending_approval SQL filter
				// which looks for tasks whose most recent message is a permission_request.
				msgID := ids.NextID()
				msg := model.Message{
					ID:          msgID,
					CreatedAt:   time.Now(),
					TaskID:      taskID,
					UserID:      monoflake.IDFromBase62(workspaceOwner).Int64(),
					Sender:      "agent",
					Text:        text,
					Attachments: datatypes.JSON(attsData),
					Metadata:    metadataJSON,
				}

				if err := repo.CreateMessage(ctx, msg); err != nil {
					return 0, err
				}

				// Notify on permission_request messages only — avoids periodic spam
				// from routine agent progress updates.
				isPermissionRequest := len(metadataJSON) > 0 && strings.Contains(string(metadataJSON), `"type":"permission_request"`)
				if isPermissionRequest {
					if w, err := repo.SystemGetWorkspace(ctx, workspaceID); err == nil {
						uid := monoflake.IDFromBase62(workspaceOwner).Int64()
						t, err := repo.GetTask(ctx, workspaceID, taskID, uid)
						if err == nil {
							notifSvc.NotifyTaskReceivedMessage(w, t, msg)
						}
					}
				}

				// Fetch updated task with messages to push to UI
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
					// Refresh task to push update to UI
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
			telemetrySvc,
		)
		srv.StartPoller(repo)
		srv.StartPing()
		return srv
	})

	// ── Fiber ──────────────────────────────────────────────────────────────────
	fiberApp := fiber.New(fiber.Config{
		DisableStartupMessage: false,
	})
	fiberApp.Use(cors.New(cors.Config{
		AllowOrigins:  "*",
		AllowHeaders:  "Origin, Content-Type, Accept, mcp-session-id, mcp-protocol-version",
		ExposeHeaders: "mcp-session-id, mcp-protocol-version",
	}))
	fiberApp.Use(logger.New())

	// No-cache headers for all static/HTML responses
	fiberApp.Use(func(c *fiber.Ctx) error {
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Set("Pragma", "no-cache")
		c.Set("Expires", "0")
		return c.Next()
	})

	// Serve static files from public directory
	fiberApp.Static("/", "./public", fiber.Static{
		Compress: false,
		Next: func(c *fiber.Ctx) bool {
			return strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/mcp")
		},
	})

	// Mount MCP handler at root (before /api/v1 group)
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

	// Mount API handler
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

	// SPA Fallback: handle all other routes by serving index.html
	fiberApp.Get("/*", func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api/") || strings.HasPrefix(c.Path(), "/mcp") {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Not Found",
			})
		}
		c.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Set("Pragma", "no-cache")
		c.Set("Expires", "0")
		return c.SendFile("./public/index.html")
	})

	// ── Unified Server Service ──────────────────────────────────────────
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

	return &App{server: serverSvc, bus: bus}, nil
}

func eventsHandler(bus *eventbus.Bus) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceIDParam := r.PathValue("id")
		var workspaceID int64
		if workspaceIDParam != "" {
			workspaceID = monoflake.IDFromBase62(workspaceIDParam).Int64()
			if workspaceID == 0 {
				http.Error(w, "invalid workspace id", http.StatusBadRequest)
				return
			}
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

		if workspaceIDParam != "" {
			fmt.Fprintf(w, ": connected to workspace %s events\n\n", workspaceIDParam)
		} else {
			fmt.Fprintf(w, ": connected to global events\n\n")
		}
		flusher.Flush()

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
