// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/acpv1"
	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

var (
	errACPUnauthorized = errors.New("not a workspace access token")
	errACPForbidden    = errors.New("no access to this workspace")
	errACPArchived     = errors.New("workspace is archived")
)

// acpWorkspaces is what the token check needs from the CRUD controller.
type acpWorkspaces interface {
	CheckWorkspaceAccess(ctx context.Context, workspaceID int64, userID string) (bool, error)
	GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error)
}

// acpTokenAuth accepts exactly the token the workspace MCP endpoint accepts:
// one minted for one workspace's access, audience [<workspace id>, "access"]
// and nothing else. A person's session, a ticket, a refresh token, an OAuth
// code and the supervisor's token each fail the audience check.
type acpTokenAuth struct {
	tokens     auth.TokenService
	workspaces acpWorkspaces
}

func (a acpTokenAuth) resolve(ctx context.Context, token string) (int64, string, error) {
	claims, err := a.tokens.ValidateToken(token)
	if err != nil || claims.Subject == "" || len(claims.Audience) != 2 || !auth.HasAudience(claims, "access") {
		return 0, "", errACPUnauthorized
	}
	var workspaceID int64
	for _, aud := range claims.Audience {
		// Only a workspace id spelled exactly as AgentRQ spells one; the
		// supervisor's "coremcp" decodes to a number too.
		if id := monoflake.IDFromBase62(aud).Int64(); aud != "access" && aud != "coremcp" && id > 0 && monoflake.ID(id).String() == aud {
			workspaceID = id
		}
	}
	if workspaceID == 0 {
		return 0, "", errACPUnauthorized
	}
	if ok, err := a.workspaces.CheckWorkspaceAccess(ctx, workspaceID, claims.Subject); err != nil || !ok {
		return 0, "", errACPForbidden
	}
	ws, err := a.workspaces.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: workspaceID, UserID: claims.Subject})
	if err != nil {
		return 0, "", errACPForbidden
	}
	if ws.Workspace.ArchivedAt != nil {
		return 0, "", errACPArchived
	}
	return workspaceID, claims.Subject, nil
}

// acpV1Auth speaks acpv1's errors, so that package stays free of app's.
type acpV1Auth struct{ acpTokenAuth }

func (a acpV1Auth) WorkspaceForToken(ctx context.Context, token string) (int64, string, error) {
	id, user, err := a.resolve(ctx, token)
	switch {
	case errors.Is(err, errACPForbidden):
		return 0, "", acpv1.ErrForbidden
	case errors.Is(err, errACPArchived):
		return 0, "", acpv1.ErrArchived
	case err != nil:
		return 0, "", acpv1.ErrUnauthorized
	}
	return id, user, nil
}

// acpCounter is what the ACP telemetry needs from the CRUD controller.
type acpCounter interface {
	RecordACPV1(ctx context.Context, req entity.RecordACPV1Request)
}

// countACPV1 builds one of the engine's telemetry hooks: kind says which
// (zero for OnConnect, Turn for OnTurn, Logout for OnLogout).
func countACPV1(rec acpCounter, kind entity.RecordACPV1Request) func(ctx context.Context, workspaceID int64, userID string) {
	return func(ctx context.Context, workspaceID int64, userID string) {
		req := kind
		req.UserID, req.WorkspaceID = monoflake.IDFromBase62(userID).Int64(), workspaceID
		rec.RecordACPV1(ctx, req)
	}
}

// acpDeps is what mounting the native ACP endpoints takes.
type acpDeps struct {
	enabled    bool
	hub        *acpv1.Hub
	auth       acpTokenAuth
	counter    acpCounter
	workspaces *mcp.Manager
	mcpURL     func(workspaceID int64) string
	appVersion string
}

// mountACP serves native ACP v1 at exactly /api/acp/v1, for acp-gateway in
// native mode. It is on the stdlib mux for the same hijacking reason as the
// other sockets, and never also a Fiber route: the mux would shadow it.
func mountACP(mux *http.ServeMux, d acpDeps) {
	mux.Handle("/api/acp/v1", &acpv1.Handler{
		Hub:     d.hub,
		Auth:    acpV1Auth{d.auth},
		Enabled: func() bool { return d.enabled },
		NewEngine: acpv1.NewEngineFactory(acpv1.EngineDeps{
			Workspace:  func(id int64, userID string) acpv1.Workspace { return d.workspaces.Get(id, userID) },
			MCPURL:     d.mcpURL,
			AppVersion: d.appVersion,
			OnConnect:  countACPV1(d.counter, entity.RecordACPV1Request{}),
			OnTurn:     countACPV1(d.counter, entity.RecordACPV1Request{Turn: true}),
			OnLogout:   countACPV1(d.counter, entity.RecordACPV1Request{Logout: true}),
		}),
	})
}
