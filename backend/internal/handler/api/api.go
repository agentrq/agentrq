package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

type (
	Params struct {
		Crud             crud.Controller
		Auth             auth.Service
		TokenSvc         auth.TokenService
		MCPManager       *mcpctrl.Manager
		EventBus         *eventbus.Bus
		BaseURL          string
		MCPBaseURL       string
		Domain           string
		SSLEnabled       bool
		TokenKey         string
		RootLoginEnabled bool
		RootToken        string
		Router           fiber.Router
	}

	Handler interface{}

	handler struct {
		crud             crud.Controller
		auth             auth.Service
		tokenSvc         auth.TokenService
		mcpManager       *mcpctrl.Manager
		bus              *eventbus.Bus
		baseURL          string
		mcpBaseURL       string
		domain           string
		sslEnabled       bool
		tokenKey         string
		rootLoginEnabled bool
		rootToken        string
		router           fiber.Router
	}
)

const (
	_routeBasePath = "/api/v1"

	_headerContentType = fiber.HeaderContentType
	_mimeJSON          = fiber.MIMEApplicationJSON
	_mimeEventStream   = "text/event-stream"
)

var _invalidPayload = []byte(`{"error":{"message":"invalid request payload","code":400}}`)

func New(p Params) (Handler, error) {
	h := &handler{
		crud:             p.Crud,
		auth:             p.Auth,
		tokenSvc:         p.TokenSvc,
		mcpManager:       p.MCPManager,
		bus:              p.EventBus,
		baseURL:          p.BaseURL,
		mcpBaseURL:       p.MCPBaseURL,
		domain:           p.Domain,
		sslEnabled:       p.SSLEnabled,
		tokenKey:         p.TokenKey,
		rootLoginEnabled: p.RootLoginEnabled,
		rootToken:        p.RootToken,
		router:           p.Router,
	}

	h.registerPublicAuthRoutes()

	// Protected routes
	h.router.Use(h.authMiddleware())

	h.registerProtectedAuthRoutes()

	if err := h.registerWorkspaceRoutes(); err != nil {
		return nil, err
	}
	if err := h.registerTaskRoutes(); err != nil {
		return nil, err
	}

	return h, nil
}

func newContext(c *fiber.Ctx) (context.Context, context.CancelFunc) {
	deadline, ok := c.Context().Deadline()
	if ok {
		return context.WithDeadline(context.Background(), deadline)
	}
	return context.WithCancel(context.Background())
}

func (h *handler) mcpURL(workspaceID int64, token string) string {
	id := monoflake.ID(workspaceID).String()
	url := fmt.Sprintf("%s/mcp/%s", h.mcpBaseURL, id)

	// If subdomain masking is possible (not localhost/IP)
	if h.domain != "" && !strings.HasPrefix(h.domain, "localhost") && !strings.HasPrefix(h.domain, "127.0.0.1") {
		proto := "https"
		if !h.sslEnabled {
			proto = "http"
		}
		// Subdomain based URLs use base36 for better compatibility (case-insensitive subdomains)
		id36 := strings.ToLower(strconv.FormatInt(workspaceID, 36))
		url = fmt.Sprintf("%s://%s.mcp.%s", proto, id36, h.domain)
	}

	if token != "" {
		url += "?token=" + token
	}
	return url
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (h *handler) registerPublicAuthRoutes() {
	r := h.router.Group("/auth")
	r.Get("/config", h.authConfig())
	r.Get("/google/login", h.googleLogin())
	r.Get("/google/callback", h.googleCallback())
	r.Post("/root/login", h.rootLogin())
}

func (h *handler) registerProtectedAuthRoutes() {
	r := h.router.Group("/auth")
	r.Get("/user", h.getAuthenticatedUser())
	r.Post("/logout", h.logout())
}

func (h *handler) logout() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Cookie(&fiber.Cookie{
			Name:     "at",
			Value:    "",
			Expires:  time.Now().Add(-1 * time.Hour),
			HTTPOnly: true,
			Path:     "/",
		})
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func (h *handler) getAuthenticatedUser() fiber.Handler {
	return func(c *fiber.Ctx) error {
		getLocalString := func(key string) string {
			if v := c.Locals(key); v != nil {
				if s, ok := v.(string); ok {
					return s
				}
			}
			return ""
		}

		userID := getLocalString("user_id")
		if userID == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "not authenticated"})
		}
		// Return full user info from locals
		return c.JSON(fiber.Map{
			"id":      userID,
			"email":   getLocalString("user_email"),
			"name":    getLocalString("user_name"),
			"picture": getLocalString("user_picture"),
		})
	}
}

// auth middleware and token generation now use internal/service/auth common logic

func (h *handler) authMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenStr := c.Cookies("at")
		if tokenStr == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		claims, err := h.tokenSvc.ValidateToken(tokenStr)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		c.Locals("user_id", claims.Subject)
		c.Locals("user_email", claims.Email)
		c.Locals("user_name", claims.Name)
		c.Locals("user_picture", claims.Picture)
		return c.Next()
	}
}

func (h *handler) authConfig() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"rootLoginEnabled": h.rootLoginEnabled,
		})
	}
}

func (h *handler) rootLogin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !h.rootLoginEnabled {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "root login disabled"})
		}

		type RootLoginRequest struct {
			RootToken string `json:"rootToken"`
		}
		var req RootLoginRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}

		if req.RootToken != h.rootToken {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid root token"})
		}

		// Issue JWT for root user
		dbUser, err := h.crud.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{
			Email: "root@agentrq.local",
			Name:  "Root Administrator",
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to sync root user"})
		}

		userID := monoflake.ID(dbUser.User.ID).String()

		tokenString, err := h.tokenSvc.CreateToken(userID, "root@agentrq.local", "Root Administrator", "")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to sign token"})
		}

		cookie := &fiber.Cookie{
			Name:     "at",
			Value:    tokenString,
			Expires:  time.Now().Add(24 * time.Hour),
			HTTPOnly: true,
			Secure:   h.sslEnabled,
			SameSite: "Lax",
			Path:     "/",
		}
		if h.domain != "" && !strings.HasPrefix(h.domain, "localhost") {
			cookie.Domain = "." + h.domain
		}
		c.Cookie(cookie)

		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func (h *handler) googleLogin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		state := c.Query("redirect_url", "state")
		return c.Redirect(h.auth.GetAuthURL(state))
	}
}

func (h *handler) googleCallback() fiber.Handler {
	return func(c *fiber.Ctx) error {
		code := c.Query("code")
		ctx, cancel := newContext(c)
		defer cancel()

		user, err := h.auth.Exchange(ctx, code)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		zlog.Info().Str("id", user.ID).Str("email", user.Email).Str("name", user.Name).Msg("OAuth code exchanged")

		sub := user.ID
		if sub == "" {
			sub = user.Sub
		}

		// Find or create user in DB
		dbUser, err := h.crud.FindOrCreateUser(ctx, entity.FindOrCreateUserRequest{
			Email:   user.Email,
			Name:    user.Name,
			Picture: user.Picture,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to sync user"})
		}

		// Use base62 ID for JWT "sub" and app-wide user identifier
		userID := monoflake.ID(dbUser.User.ID).String()

		// Create JWT using centralized logic
		tokenString, err := h.tokenSvc.CreateToken(userID, user.Email, user.Name, user.Picture)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to sign token"})
		}

		cookie := &fiber.Cookie{
			Name:     "at",
			Value:    tokenString,
			Expires:  time.Now().Add(24 * time.Hour),
			HTTPOnly: true,
			Secure:   h.sslEnabled,
			SameSite: "Lax",
			Path:     "/",
		}
		if h.domain != "" && !strings.HasPrefix(h.domain, "localhost") {
			cookie.Domain = "." + h.domain
		}
		c.Cookie(cookie)

		state := c.Query("state")
		redirectURL := "/"
		// Situational security: validate redirect URL to prevent open redirect
		if state != "" && state != "state" {
			if strings.HasPrefix(state, "/") && !strings.HasPrefix(state, "//") {
				redirectURL = state
			} else if strings.HasPrefix(state, h.baseURL) {
				redirectURL = state
			}
		}

		return c.Redirect(redirectURL)
	}
}

// ── Workspaces ──────────────────────────────────────────────────────────────────

func (h *handler) registerWorkspaceRoutes() error {
	r := h.router.Group("/workspaces")
	r.Post("", h.createWorkspace())
	r.Get("", h.listWorkspaces())
	r.Get("/:id", h.getWorkspace())
	r.Get("/:id/token", h.getWorkspaceToken())
	r.Delete("/:id", h.deleteWorkspace())
	r.Patch("/:id", h.updateWorkspace())
	r.Post("/:id/archive", h.archiveWorkspace())
	r.Post("/:id/unarchive", h.unarchiveWorkspace())
	r.Get("/:id/stats", h.getWorkspaceStats())
	return nil
}

func (h *handler) createWorkspace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToCreateWorkspaceRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.CreateWorkspace(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		rs.Workspace.AgentConnected = h.mcpManager.IsAgentConnected(rs.Workspace.ID)

		// Decrypt situational secret for mission owner visibility
		token := ""
		if rs.Workspace.TokenEncrypted != "" {
			dec, _ := security.Decrypt(rs.Workspace.TokenEncrypted, h.tokenKey, rs.Workspace.TokenNonce)
			token = dec
		}

		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateWorkspaceResponseEntityToHTTPResponse(rs, h.mcpURL(rs.Workspace.ID, token)))
	}
}

func (h *handler) getWorkspace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetWorkspaceRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetWorkspace(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		rs.Workspace.AgentConnected = h.mcpManager.IsAgentConnected(rs.Workspace.ID)

		// Decrypt situational secret for mission owner visibility
		token := ""
		if rs.Workspace.TokenEncrypted != "" {
			dec, _ := security.Decrypt(rs.Workspace.TokenEncrypted, h.tokenKey, rs.Workspace.TokenNonce)
			token = dec
		}

		c.Status(http.StatusOK)
		return c.Send(mapper.FromGetWorkspaceResponseEntityToHTTPResponse(rs, h.mcpURL(rs.Workspace.ID, token)))
	}
}

func (h *handler) listWorkspaces() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		archived := c.Query("archived") == "true"
		rs, err := h.crud.ListWorkspaces(ctx, entity.ListWorkspacesRequest{
			UserID:          c.Locals("user_id").(string),
			IncludeArchived: archived,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		for i := range rs.Workspaces {
			rs.Workspaces[i].AgentConnected = h.mcpManager.IsAgentConnected(rs.Workspaces[i].ID)
		}

		mcpURLWithToken := func(workspaceID int64) string {
			// For list, we generally don't include plain token unless strictly situational required.
			// However, since missionowner is viewing THEIR workspaces, we can include it.
			// Optimized: We would need the full workspace model here.
			// For now, list will show generic URL to save decryption cost,
			// detail will show full URL.
			return h.mcpURL(workspaceID, "")
		}

		c.Status(http.StatusOK)
		return c.Send(mapper.FromListWorkspacesResponseEntityToHTTPResponse(rs, mcpURLWithToken))
	}
}

func (h *handler) deleteWorkspace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteWorkspaceRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeleteWorkspace(ctx, *rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		h.mcpManager.Remove(rq.ID)
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}
func (h *handler) archiveWorkspace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if workspaceID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)
		rq := entity.ArchiveWorkspaceRequest{ID: workspaceID, UserID: userID}
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.ArchiveWorkspace(ctx, rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusOK)
		return c.JSON(fiber.Map{"status": "archived"})
	}
}

func (h *handler) unarchiveWorkspace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if workspaceID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)
		rq := entity.UnarchiveWorkspaceRequest{ID: workspaceID, UserID: userID}
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.UnarchiveWorkspace(ctx, rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		// Refresh MCP server state if running
		if srv := h.mcpManager.Get(workspaceID, userID); srv != nil {
			srv.UpdateArchivedAt(nil)
		}
		c.Status(http.StatusOK)
		return c.JSON(fiber.Map{"status": "unarchived"})
	}
}

func (h *handler) updateWorkspace() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateWorkspaceRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateWorkspace(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		// Update running MCP server metadata
		if srv := h.mcpManager.Get(rq.Workspace.ID, rq.UserID); srv != nil {
			srv.UpdateMetadata(rs.Workspace.Name, rs.Workspace.Description, rs.Workspace.Icon)
			srv.UpdateAutoAllowedTools(rs.Workspace.AutoAllowedTools)
		}
		rs.Workspace.AgentConnected = h.mcpManager.IsAgentConnected(rq.Workspace.ID)

		// Decrypt situational secret for mission owner visibility
		token := ""
		if rs.Workspace.TokenEncrypted != "" {
			dec, _ := security.Decrypt(rs.Workspace.TokenEncrypted, h.tokenKey, rs.Workspace.TokenNonce)
			token = dec
		}

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateWorkspaceResponseEntityToHTTPResponse(&rs.Workspace, h.mcpURL(rq.Workspace.ID, token)))
	}
}

func (h *handler) getWorkspaceToken() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := c.Params("id")
		userID := c.Locals("user_id").(string)

		token, err := h.tokenSvc.CreateMCPToken(userID, workspaceID, "access")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate workspace token"})
		}
		return c.JSON(fiber.Map{"token": token})
	}
}

func (h *handler) getWorkspaceStats() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		workspace64 := monoflake.IDFromBase62(c.Params("id")).Int64()
		if workspace64 == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)

		rng := c.Query("range", "7d")
		from, _ := strconv.ParseInt(c.Query("from"), 10, 64)
		to, _ := strconv.ParseInt(c.Query("to"), 10, 64)

		rq := entity.GetWorkspaceStatsRequest{
			ID:     workspace64,
			UserID: userID,
			Range:  rng,
			From:   from,
			To:     to,
		}

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetDetailedWorkspaceStats(ctx, rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Status(http.StatusOK).JSON(rs)
	}
}
