// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package teamview

import (
	"context"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
)

func (c *controller) ListPanoDocs(ctx context.Context, userID, teamID int64) ([]entity.TeamPanoDoc, error) {
	if _, err := c.teams.GetTeam(ctx, userID, teamID); err != nil {
		return nil, err
	}
	rows, err := c.repo.ListTeamPanoDocs(ctx, teamID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	out := make([]entity.TeamPanoDoc, len(rows))
	for i, d := range rows {
		out[i] = entity.TeamPanoDoc{
			PanoDoc: entity.PanoDoc{
				ID: d.ID, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Title: d.Title, Excerpt: d.Excerpt,
				TTLDays: d.TTLDays, ExpiresAt: d.ExpiresAt, Color: entity.PanoColor(d.Color),
				ChecklistDone: d.ChecklistDone, ChecklistTotal: d.ChecklistTotal, Version: d.Version,
			},
			OwnerID: d.UserID, OwnerName: d.OwnerName, OwnerPicture: d.OwnerPicture,
		}
	}
	return out, nil
}

// panoOwner is the owner of a doc the user reads in the team; every read
// of a shared doc starts here.
func (c *controller) panoOwner(ctx context.Context, userID, teamID, docID int64) (base.PanoDocOwner, string, error) {
	o, err := c.repo.PanoDocOwnerInTeam(ctx, userID, teamID, docID)
	return o, monoflake.ID(o.UserID).String(), err
}

func (c *controller) GetPanoDoc(ctx context.Context, userID, teamID, docID int64) (entity.TeamPanoDoc, error) {
	o, owner, err := c.panoOwner(ctx, userID, teamID, docID)
	if err != nil {
		return entity.TeamPanoDoc{}, err
	}
	rs, err := c.owner.GetPanoDoc(ctx, entity.GetPanoDocRequest{UserID: owner, ID: docID, IncludeAttachments: true})
	if err != nil {
		return entity.TeamPanoDoc{}, err
	}
	d := rs.Doc
	d.Tasks = nil
	d.CreatedByWorkspaceID, d.UpdatedByWorkspaceID = 0, 0
	for _, files := range [][]entity.PanoFile{d.Files, d.Attachments} {
		for i := range files {
			files[i].URL = TeamPanoFileURL(teamID, docID, files[i].ID)
			files[i].WorkspaceID = 0
		}
	}
	return entity.TeamPanoDoc{PanoDoc: d, OwnerID: o.UserID, OwnerName: o.Name, OwnerPicture: o.Picture}, nil
}

func (c *controller) GetPanoFile(ctx context.Context, userID, teamID, docID, fileID int64) (*entity.GetAttachmentResponse, error) {
	_, owner, err := c.panoOwner(ctx, userID, teamID, docID)
	if err != nil {
		return nil, err
	}
	return c.owner.GetPanoFile(ctx, entity.GetPanoFileRequest{UserID: owner, DocID: docID, FileID: fileID})
}

// TeamPanoFileURL is the link a teammate reads a shared doc's file by. The
// public link has no session, so it would outlive the share.
func TeamPanoFileURL(teamID, docID, fileID int64) string {
	return "/api/v1/teams/" + monoflake.ID(teamID).String() + "/pano/" + monoflake.ID(docID).String() +
		"/files/" + monoflake.ID(fileID).String()
}
