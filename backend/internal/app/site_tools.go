// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/controller/sitetools"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

// siteToolsBackend adapts the repository and the hub to mcp.SiteToolsBackend.
type siteToolsBackend struct {
	repo base.Repository
	hub  *sitetools.Hub
}

func (b siteToolsBackend) view(userID int64, r model.SiteShare) mcp.SiteShareView {
	v := mcp.SiteShareView{Site: r.Origin, BrowserID: r.BrowserID, InstanceID: r.InstanceID}
	_ = json.Unmarshal([]byte(r.Tools), &v.Tools)
	_ = json.Unmarshal([]byte(r.AlwaysAllow), &v.AlwaysAllow)
	if v.Tools == nil {
		v.Tools = []sitetools.Tool{}
	}
	v.Online = r.InstanceID == b.hub.InstanceID() && b.hub.Online(userID, r.BrowserID)
	return v
}

func (b siteToolsBackend) List(ctx context.Context, workspaceID, userID int64) ([]mcp.SiteShareView, error) {
	rows, err := b.repo.ListSiteSharesForWorkspace(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]mcp.SiteShareView, 0, len(rows))
	for _, r := range rows {
		out = append(out, b.view(userID, r))
	}
	return out, nil
}

func (b siteToolsBackend) Get(ctx context.Context, workspaceID, userID int64, origin string) (mcp.SiteShareView, bool, error) {
	r, err := b.repo.GetSiteShare(ctx, workspaceID, userID, origin)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mcp.SiteShareView{}, false, nil
	}
	if err != nil {
		return mcp.SiteShareView{}, false, err
	}
	return b.view(userID, r), true, nil
}

func (b siteToolsBackend) AllowAlways(ctx context.Context, workspaceID, userID int64, origin, tool string) error {
	r, err := b.repo.GetSiteShare(ctx, workspaceID, userID, origin)
	if err != nil {
		return err
	}
	var names []string
	_ = json.Unmarshal([]byte(r.AlwaysAllow), &names)
	if slices.Contains(names, tool) {
		return nil
	}
	return b.repo.SetSiteShareAlwaysAllow(ctx, r.ID, append(names, tool))
}

// Call routes through this instance's hub. A browser whose socket another
// instance holds cannot be reached from here: calls are never relayed.
func (b siteToolsBackend) Call(ctx context.Context, userID int64, share mcp.SiteShareView, tool string, args json.RawMessage) (string, error) {
	if share.InstanceID != b.hub.InstanceID() && !b.hub.Online(userID, share.BrowserID) {
		return "", mcp.ErrSiteOtherInstance
	}
	f, err := b.hub.Call(ctx, userID, share.BrowserID, share.Site, tool, args)
	if err != nil {
		return "", err
	}
	if f.Error != "" {
		return "", &mcp.SiteToolFailedError{Message: f.Error}
	}
	return f.Text, nil
}
