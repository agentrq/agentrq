// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/sitetools"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
)

// siteShareStore adapts the repository to sitetools.Store.
type siteShareStore struct {
	repo base.Repository
	ids  idgen.Service
}

func (s siteShareStore) OwnsWorkspace(ctx context.Context, userID, workspaceID int64) (bool, error) {
	return s.repo.CheckWorkspaceAccess(ctx, workspaceID, userID)
}

func (s siteShareStore) Upsert(ctx context.Context, sh sitetools.Share) (bool, error) {
	// A fork lists its parent's shares, so a site shared with a fork is filed
	// under the parent: there is one home for it, and a merge leaves none behind.
	w, err := s.repo.SystemGetWorkspace(ctx, sh.WorkspaceID)
	if err != nil {
		return false, err
	}
	sh.WorkspaceID = w.ContentID()
	prev, found, err := s.find(ctx, sh.UserID, sh.Origin)
	if err != nil {
		return false, err
	}
	tools := sh.Tools
	if tools == nil {
		tools = []sitetools.Tool{}
	}
	b, _ := json.Marshal(tools)
	// The id is kept on conflict, so a fresh one is only used by a new row.
	_, err = s.repo.UpsertSiteShare(ctx, model.SiteShare{
		ID:          s.ids.NextID(),
		UserID:      sh.UserID,
		WorkspaceID: sh.WorkspaceID,
		Origin:      sh.Origin,
		BrowserID:   sh.BrowserID,
		InstanceID:  sh.InstanceID,
		LastURL:     sh.LastURL,
		Tools:       string(b),
		AlwaysAllow: "[]",
	})
	if err != nil {
		return false, err
	}
	return !found || prev.WorkspaceID != sh.WorkspaceID, nil
}

func (s siteShareStore) Delete(ctx context.Context, userID int64, origin string) (int64, bool, error) {
	prev, _, err := s.find(ctx, userID, origin)
	if err != nil {
		return 0, false, err
	}
	deleted, err := s.repo.DeleteSiteShare(ctx, userID, origin)
	return prev.WorkspaceID, deleted, err
}

func (s siteShareStore) ListForUser(ctx context.Context, userID int64) ([]sitetools.Share, error) {
	rows, err := s.repo.ListSiteSharesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]sitetools.Share, 0, len(rows))
	for _, r := range rows {
		sh := sitetools.Share{
			UserID: r.UserID, WorkspaceID: r.WorkspaceID, Origin: r.Origin,
			BrowserID: r.BrowserID, InstanceID: r.InstanceID, LastURL: r.LastURL,
		}
		_ = json.Unmarshal([]byte(r.Tools), &sh.Tools)
		_ = json.Unmarshal([]byte(r.AlwaysAllow), &sh.AlwaysAllow)
		out = append(out, sh)
	}
	return out, nil
}

// find is the user's share of origin. The repository has no lookup by origin
// alone, and an account shares few sites.
func (s siteShareStore) find(ctx context.Context, userID int64, origin string) (model.SiteShare, bool, error) {
	rows, err := s.repo.ListSiteSharesForUser(ctx, userID)
	if err != nil {
		return model.SiteShare{}, false, err
	}
	for _, r := range rows {
		if r.Origin == origin {
			return r, true, nil
		}
	}
	return model.SiteShare{}, false, nil
}

// browserTicketAuth authenticates the browser socket by its ?ticket=, never
// the cookie, which the extension's origin does not receive.
func browserTicketAuth(tokens auth.TokenService) func(*http.Request) (int64, error) {
	return func(r *http.Request) (int64, error) {
		claims, err := tokens.ValidateBrowserTicket(r.URL.Query().Get("ticket"))
		if err != nil {
			return 0, err
		}
		userID := monoflake.IDFromBase62(claims.Subject).Int64()
		if userID == 0 {
			return 0, fmt.Errorf("app: unreadable subject")
		}
		return userID, nil
	}
}

type siteShareCounter interface {
	RecordSiteShare(context.Context, entity.RecordSiteShareRequest)
}

// countSiteShare builds the socket's OnShare (shared) or OnUnshare hook.
func countSiteShare(rec siteShareCounter, shared bool) func(userID, workspaceID int64, origin string) {
	return func(userID, workspaceID int64, _ string) {
		rec.RecordSiteShare(context.Background(), entity.RecordSiteShareRequest{
			UserID: userID, WorkspaceID: workspaceID, Shared: shared,
		})
	}
}
