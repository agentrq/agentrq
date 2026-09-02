package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	pushctrl "github.com/agentrq/agentrq/backend/internal/controller/push"
	slackctrl "github.com/agentrq/agentrq/backend/internal/controller/slack"
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
		GithubAuth       auth.Service
		GithubClientID   string
		TokenSvc         auth.TokenService
		MCPManager       *mcpctrl.Manager
		EventBus         *eventbus.Bus
		BaseURL          string
		MCPBaseURL       string
		Domain           string
		CookieSecure     bool
		BasePath         string
		RootLoginEnabled bool
		RootToken        string
		Router           fiber.Router
		SlackCtrl        slackctrl.Controller // optional; nil = Slack disabled
		PushCtrl         pushctrl.Controller  // optional; nil = push disabled
	}

	Handler interface{}

	handler struct {
		crud             crud.Controller
		auth             auth.Service
		githubAuth       auth.Service
		githubClientID   string
		tokenSvc         auth.TokenService
		mcpManager       *mcpctrl.Manager
		bus              *eventbus.Bus
		baseURL          string
		mcpBaseURL       string
		domain           string
		cookieSecure     bool
		basePath         string
		rootLoginEnabled bool
		rootToken        string
		router           fiber.Router
		slackCtrl        slackctrl.Controller
		pushCtrl         pushctrl.Controller
	}
)

const (
	_routeBasePath = "/api/v1"

	// Cookie names. `at` is sent with every request; `rt` is scoped to the one
	// route that consumes it, so the long-lived credential is not attached to
	// every API call the app makes.
	_accessCookie  = "at"
	_refreshCookie = "rt"

	_accessTokenTTL = 24 * time.Hour

	_headerContentType = fiber.HeaderContentType
	_mimeJSON          = fiber.MIMEApplicationJSON
	_mimeEventStream   = "text/event-stream"
)

var _invalidPayload = []byte(`{"error":{"message":"invalid request payload","code":400}}`)

func New(p Params) (Handler, error) {
	h := &handler{
		crud:             p.Crud,
		auth:             p.Auth,
		githubAuth:       p.GithubAuth,
		githubClientID:   p.GithubClientID,
		tokenSvc:         p.TokenSvc,
		mcpManager:       p.MCPManager,
		bus:              p.EventBus,
		baseURL:          p.BaseURL,
		mcpBaseURL:       p.MCPBaseURL,
		domain:           p.Domain,
		cookieSecure:     p.CookieSecure,
		basePath:         p.BasePath,
		rootLoginEnabled: p.RootLoginEnabled,
		rootToken:        p.RootToken,
		router:           p.Router,
		slackCtrl:        p.SlackCtrl,
		pushCtrl:         p.PushCtrl,
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
	h.registerEventRoutes()
	h.registerWorkflowRoutes()
	if err := h.registerTelemetryRoutes(); err != nil {
		return nil, err
	}
	if p.PushCtrl != nil {
		h.registerPushRoutes(p.PushCtrl)
	}

	return h, nil
}

func newContext(c *fiber.Ctx) (context.Context, context.CancelFunc) {
	withLocals := func(ctx context.Context) context.Context {
		if claims, ok := c.Locals("claims").(*auth.Claims); ok && claims != nil {
			ctx = context.WithValue(ctx, auth.CtxKeyMCPClaims, claims)
		}
		return ctx
	}
	deadline, ok := c.Context().Deadline()
	if ok {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		return withLocals(ctx), cancel
	}
	ctx, cancel := context.WithCancel(context.Background())
	return withLocals(ctx), cancel
}

func (h *handler) mcpURL(workspaceID int64) string {
	id := monoflake.ID(workspaceID).String()
	url := fmt.Sprintf("%s/mcp/%s", h.mcpBaseURL, id)

	// If subdomain masking is possible (not localhost/IP)
	if h.domain != "" && !strings.HasPrefix(h.domain, "localhost") && !strings.HasPrefix(h.domain, "127.0.0.1") {
		proto := "https"
		if !h.cookieSecure {
			proto = "http"
		}
		// Subdomain based URLs use base36 for better compatibility (case-insensitive subdomains)
		id36 := strings.ToLower(strconv.FormatInt(workspaceID, 36))
		url = fmt.Sprintf("%s://%s.mcp.%s", proto, id36, h.domain)
	}

	return url
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func (h *handler) registerPublicAuthRoutes() {
	r := h.router.Group("/auth")
	r.Get("/config", h.authConfig())
	r.Get("/google/login", h.googleLogin())
	r.Get("/google/callback", h.googleCallback())
	r.Get("/github/login", h.githubLogin())
	r.Get("/github/callback", h.githubCallback())
	r.Post("/root/login", h.rootLogin())
	// Public on purpose: the whole point is that the access token has expired,
	// so this route cannot sit behind the middleware that checks it. It
	// authenticates with the refresh cookie instead.
	r.Post("/refresh", h.refreshSession())
}

func (h *handler) registerProtectedAuthRoutes() {
	r := h.router.Group("/auth")
	r.Get("/user", h.getAuthenticatedUser())
	r.Post("/logout", h.logout())
}

// refreshCookiePath scopes the refresh cookie to the route that reads it.
//
// Derived from the same base path the routes are mounted under, plus the SPA
// base path a reverse-proxied deployment adds in front. Getting this wrong
// fails silently — the browser simply never sends the cookie, and sessions
// expire exactly as they did before the refresh flow existed — so there is a
// test asserting it matches where the route actually lives.
func (h *handler) refreshCookiePath() string {
	return h.basePath + _routeBasePath + "/auth/refresh"
}

// sessionCookie applies the settings every session cookie shares.
func (h *handler) sessionCookie(name, value string, expires time.Time, path string) *fiber.Cookie {
	cookie := &fiber.Cookie{
		Name:     name,
		Value:    value,
		Expires:  expires,
		HTTPOnly: true,
		Secure:   h.cookieSecure,
		SameSite: "Lax",
		Path:     path,
	}
	if h.domain != "" && !strings.HasPrefix(h.domain, "localhost") {
		cookie.Domain = "." + h.domain
	}
	return cookie
}

// issueSession sets both halves of a session.
//
// One function rather than a block copied into each sign-in path: three of
// those already existed, and a fourth that set only the access cookie would
// leave that provider's users signed out after a day with nothing to show why.
func (h *handler) issueSession(c *fiber.Ctx, accessToken, userID string) {
	now := time.Now()
	c.Cookie(h.sessionCookie(_accessCookie, accessToken, now.Add(_accessTokenTTL), "/"))

	refreshToken, err := h.tokenSvc.CreateRefreshToken(userID)
	if err != nil {
		// Deliberately not fatal. The person is signed in either way; without
		// this they simply have the session they would have had before refresh
		// tokens existed, which is a far better outcome than refusing a login
		// that otherwise succeeded.
		zlog.Error().Err(err).Msg("Failed to mint refresh token; session will not survive expiry")
		return
	}
	c.Cookie(h.sessionCookie(_refreshCookie, refreshToken, now.Add(auth.RefreshTokenTTL), h.refreshCookiePath()))
}

// clearSession expires both halves. The refresh cookie has to be cleared at the
// path it was set on; a deletion at "/" would not match it and would leave the
// session renewable after signing out.
func (h *handler) clearSession(c *fiber.Ctx) {
	expired := time.Now().Add(-1 * time.Hour)
	c.Cookie(h.sessionCookie(_accessCookie, "", expired, "/"))
	c.Cookie(h.sessionCookie(_refreshCookie, "", expired, h.refreshCookiePath()))
}

// refreshSession exchanges a valid refresh cookie for a fresh session.
//
// Both cookies are reissued, so a session in regular use never reaches the idle
// limit while one that is genuinely abandoned still expires.
func (h *handler) refreshSession() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)

		claims, err := h.tokenSvc.ValidateRefreshToken(c.Cookies(_refreshCookie))
		if err != nil || claims == nil {
			// Missing or expired is the ordinary end of a session, not a fault.
			// Clearing both cookies stops the client retrying with a credential
			// that will never work again.
			h.clearSession(c)
			return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "session expired"})
		}

		ctx, cancel := newContext(c)
		defer cancel()

		user, err := h.crud.FindUserByID(ctx, monoflake.IDFromBase62(claims.Subject).Int64())
		if err != nil {
			// A token that outlived the account it names.
			h.clearSession(c)
			return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "session expired"})
		}

		userID := monoflake.ID(user.ID).String()
		accessToken, err := h.tokenSvc.CreateToken(userID, user.Email, user.Name, user.Picture)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to mint access token on refresh")
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "could not refresh session"})
		}

		h.issueSession(c, accessToken, userID)
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func (h *handler) logout() fiber.Handler {
	return func(c *fiber.Ctx) error {
		h.clearSession(c)
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
		c.Locals("claims", claims)
		return c.Next()
	}
}

func (h *handler) authConfig() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"rootLoginEnabled":   h.rootLoginEnabled,
			"githubLoginEnabled": h.githubClientID != "",
			"basePath":           h.basePath,
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

		if h.rootToken == "" || !security.SecureCompare(req.RootToken, h.rootToken) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid root token"})
		}

		// Issue JWT for root user
		dbUser, err := h.crud.FindOrCreateUser(context.Background(), entity.FindOrCreateUserRequest{
			Email: "root@agentrq.local",
			Name:  "Root Administrator",
		})
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to sync root user")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		userID := monoflake.ID(dbUser.User.ID).String()

		tokenString, err := h.tokenSvc.CreateToken(userID, "root@agentrq.local", "Root Administrator", "")
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to sign root token")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		h.issueSession(c, tokenString, userID)

		return c.JSON(fiber.Map{"status": "ok"})
	}
}

func (h *handler) googleLogin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		defaultRedirect := "/"
		if h.basePath != "" {
			defaultRedirect = h.basePath + "/"
		}
		redirectURL := h.sanitizeRedirectURL(c.Query("redirect_url", defaultRedirect))
		state, err := h.tokenSvc.CreateOAuthStateToken(redirectURL, "google")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate state"})
		}
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
			zlog.Error().Err(err).Msg("OAuth exchange failed")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
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
			zlog.Error().Err(err).Msg("Failed to sync user")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Use base62 ID for JWT "sub" and app-wide user identifier
		userID := monoflake.ID(dbUser.User.ID).String()

		// Create JWT using centralized logic
		tokenString, err := h.tokenSvc.CreateToken(userID, user.Email, user.Name, user.Picture)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to sign token")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		h.issueSession(c, tokenString, userID)

		redirectURL := "/"
		if h.basePath != "" {
			redirectURL = h.basePath + "/"
		}
		if stateToken := c.Query("state"); stateToken != "" {
			if rurl, err := h.tokenSvc.ValidateOAuthStateToken(stateToken, "google"); err == nil {
				redirectURL = h.sanitizeRedirectURL(rurl)
			}
		}

		return c.Redirect(redirectURL)
	}
}

func (h *handler) githubLogin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.githubClientID == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "github login disabled"})
		}
		defaultRedirect := "/"
		if h.basePath != "" {
			defaultRedirect = h.basePath + "/"
		}
		redirectURL := h.sanitizeRedirectURL(c.Query("redirect_url", defaultRedirect))
		state, err := h.tokenSvc.CreateOAuthStateToken(redirectURL, "github")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to generate state"})
		}
		return c.Redirect(h.githubAuth.GetAuthURL(state))
	}
}

func (h *handler) githubCallback() fiber.Handler {
	return func(c *fiber.Ctx) error {
		code := c.Query("code")
		ctx, cancel := newContext(c)
		defer cancel()

		user, err := h.githubAuth.Exchange(ctx, code)
		if err != nil {
			zlog.Error().Err(err).Msg("GitHub OAuth exchange failed")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		zlog.Info().Str("id", user.ID).Str("email", user.Email).Str("name", user.Name).Msg("GitHub OAuth code exchanged")

		dbUser, err := h.crud.FindOrCreateUser(ctx, entity.FindOrCreateUserRequest{
			Email:   user.Email,
			Name:    user.Name,
			Picture: user.Picture,
		})
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to sync GitHub user")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		userID := monoflake.ID(dbUser.User.ID).String()

		tokenString, err := h.tokenSvc.CreateToken(userID, user.Email, user.Name, user.Picture)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to sign GitHub token")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		h.issueSession(c, tokenString, userID)

		redirectURL := "/"
		if h.basePath != "" {
			redirectURL = h.basePath + "/"
		}
		if stateToken := c.Query("state"); stateToken != "" {
			if rurl, err := h.tokenSvc.ValidateOAuthStateToken(stateToken, "github"); err == nil {
				redirectURL = h.sanitizeRedirectURL(rurl)
			}
		}

		return c.Redirect(redirectURL)
	}
}

// sanitizeRedirectURL validates a redirect URL to prevent open redirect attacks.
// Only same-origin relative paths (starting with /) or absolute URLs matching
// the configured baseURL are accepted; everything else falls back to /.
func (h *handler) sanitizeRedirectURL(raw string) string {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.HasPrefix(raw, "/\\") {
		return raw
	}
	if pRedirect, err := url.Parse(raw); err == nil && pRedirect.IsAbs() {
		if pBase, err := url.Parse(h.baseURL); err == nil {
			if pRedirect.Host == pBase.Host && pRedirect.Scheme == pBase.Scheme {
				return raw
			}
		}
	}
	if h.basePath != "" {
		return h.basePath + "/"
	}
	return "/"
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
	r.Put("/:id/slack", h.setWorkspaceSlackChannel())
	r.Delete("/:id/slack", h.removeWorkspaceSlackChannel())
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
			zlog.Error().Err(err).Msg("Failed to create workspace")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		rs.Workspace.AgentConnected = h.mcpManager.IsAgentConnected(rs.Workspace.ID)
		rs.Workspace.AgentSupportsStop = h.mcpManager.SupportsStop(rs.Workspace.ID)
		h.enrichWorkspaceSlack(ctx, &rs.Workspace)

		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateWorkspaceResponseEntityToHTTPResponse(rs, h.mcpURL(rs.Workspace.ID)))
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
			zlog.Error().Err(err).Msg("Failed to get workspace")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		rs.Workspace.AgentConnected = h.mcpManager.IsAgentConnected(rs.Workspace.ID)
		rs.Workspace.AgentSupportsStop = h.mcpManager.SupportsStop(rs.Workspace.ID)
		h.enrichWorkspaceSlack(ctx, &rs.Workspace)

		c.Status(http.StatusOK)
		return c.Send(mapper.FromGetWorkspaceResponseEntityToHTTPResponse(rs, h.mcpURL(rs.Workspace.ID)))
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
			zlog.Error().Err(err).Msg("Failed to list workspaces")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		for i := range rs.Workspaces {
			rs.Workspaces[i].AgentConnected = h.mcpManager.IsAgentConnected(rs.Workspaces[i].ID)
			rs.Workspaces[i].AgentSupportsStop = h.mcpManager.SupportsStop(rs.Workspaces[i].ID)
			h.enrichWorkspaceSlack(ctx, &rs.Workspaces[i])
		}

		c.Status(http.StatusOK)
		return c.Send(mapper.FromListWorkspacesResponseEntityToHTTPResponse(rs, h.mcpURL))
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
			zlog.Error().Err(err).Msg("Failed to delete workspace")
			c.Set(_headerContentType, _mimeJSON)
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
			zlog.Error().Err(err).Msg("Failed to archive workspace")
			c.Set(_headerContentType, _mimeJSON)
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
			zlog.Error().Err(err).Msg("Failed to unarchive workspace")
			c.Set(_headerContentType, _mimeJSON)
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
			zlog.Error().Err(err).Msg("Failed to update workspace")
			c.Set(_headerContentType, _mimeJSON)
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
		rs.Workspace.AgentSupportsStop = h.mcpManager.SupportsStop(rq.Workspace.ID)
		h.enrichWorkspaceSlack(ctx, &rs.Workspace)

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateWorkspaceResponseEntityToHTTPResponse(&rs.Workspace, h.mcpURL(rq.Workspace.ID)))
	}
}

func (h *handler) getWorkspaceToken() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := c.Params("id")
		userID := c.Locals("user_id").(string)

		// Authorization: verify that the user has access to this workspace
		workspace64 := monoflake.IDFromBase62(workspaceID).Int64()
		if workspace64 == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		ctx, cancel := newContext(c)
		defer cancel()

		_, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{
			ID:     workspace64,
			UserID: userID,
		})
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to verify workspace access for token")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		token, err := h.tokenSvc.CreateMCPToken(userID, workspaceID, "access")
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to generate workspace token")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
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
			zlog.Error().Err(err).Msg("Failed to get workspace stats")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Status(http.StatusOK).JSON(rs)
	}
}

func (h *handler) setWorkspaceSlackChannel() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if workspaceID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)

		var body struct {
			ChannelID   string `json:"channelId"`
			ChannelName string `json:"channelName"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.ChannelID == "" || body.ChannelName == "" {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "channelId and channelName are required"})
		}

		ctx, cancel := newContext(c)
		defer cancel()

		if h.slackCtrl == nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Slack integration is not enabled"})
		}

		err := h.slackCtrl.SetWorkspaceChannel(ctx, entity.SetWorkspaceSlackChannelRequest{
			WorkspaceID: workspaceID,
			UserID:      userID,
			ChannelID:   body.ChannelID,
			ChannelName: body.ChannelName,
			AutoCreated: false,
		})
		if err != nil {
			zlog.Error().Err(err).Int64("workspace_id", workspaceID).Msg("Failed to set workspace Slack channel")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			return c.Status(status).Send(e)
		}

		return c.Status(http.StatusOK).JSON(fiber.Map{"status": "success"})
	}
}

func (h *handler) removeWorkspaceSlackChannel() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if workspaceID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()

		if h.slackCtrl == nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "Slack integration is not enabled"})
		}

		err := h.slackCtrl.RemoveWorkspaceChannel(ctx, entity.RemoveWorkspaceSlackChannelRequest{
			WorkspaceID: workspaceID,
			UserID:      userID,
		})
		if err != nil {
			zlog.Error().Err(err).Int64("workspace_id", workspaceID).Msg("Failed to remove workspace Slack channel")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			return c.Status(status).Send(e)
		}

		return c.Status(http.StatusNoContent).Send([]byte(""))
	}
}

func (h *handler) enrichWorkspaceSlack(ctx context.Context, ws *entity.Workspace) {
	if h.slackCtrl == nil || ws == nil {
		return
	}
	cfg, err := h.slackCtrl.GetWorkspaceSlackConfig(ctx, ws.ID)
	if err == nil && cfg != nil {
		ws.Slack = cfg
	}
}
