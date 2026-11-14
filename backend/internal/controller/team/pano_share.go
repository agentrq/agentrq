// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

func panoShareEntity(s base.PanoDocShareWithTeam) entity.PanoDocShare {
	return entity.PanoDocShare{TeamID: s.TeamID, TeamName: s.TeamName, DocID: s.DocID, CreatedAt: s.CreatedAt}
}

// panoDoc is the account's own Pano doc, while it has not expired.
func (c *controller) panoDoc(ctx context.Context, userID, docID int64) (model.PanoDoc, error) {
	return c.repo.GetPanoDoc(ctx, userID, docID, c.now())
}

// panoActivity is a new activity that names the doc as it was titled then,
// which the log keeps after it is renamed or deleted.
func (c *controller) panoActivity(d model.PanoDoc) model.TeamManagementActivity {
	act := c.activity()
	act.Details = base.TeamActivityDetails(map[string]string{"docTitle": d.Title})
	return act
}

func (c *controller) ListPanoShares(ctx context.Context, userID, docID int64) ([]entity.PanoDocShare, error) {
	if _, err := c.panoDoc(ctx, userID, docID); err != nil {
		return nil, err
	}
	rows, err := c.repo.ListPanoDocShares(ctx, docID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.PanoDocShare, len(rows))
	for i, s := range rows {
		out[i] = panoShareEntity(s)
	}
	return out, nil
}

// SharePanoDoc shares the account's Pano doc with a team it is in, for its
// members to read. The repository checks the account is in the team, and
// the doc is still there, under the team's lock.
func (c *controller) SharePanoDoc(ctx context.Context, ch entity.TeamChange, docID int64) (entity.PanoDocShare, error) {
	d, err := c.panoDoc(ctx, ch.UserID, docID)
	if err != nil {
		return entity.PanoDocShare{}, err
	}
	s, next, err := c.repo.SetPanoDocShare(ctx, model.PanoDocShare{
		ID: c.ids.NextID(), TeamID: ch.TeamID, DocID: docID, UserID: ch.UserID, CreatedAt: c.now(),
	}, ch.Version, c.panoActivity(d))
	if err != nil {
		return entity.PanoDocShare{}, fromRepo(err)
	}
	c.emit(ctx, entity.ActionPanoShare, ch.UserID, 0, ch.TeamID)
	out := panoShareEntity(s)
	out.Version = next
	return out, nil
}

func (c *controller) UnsharePanoDoc(ctx context.Context, ch entity.TeamChange, docID int64) (int64, error) {
	d, err := c.panoDoc(ctx, ch.UserID, docID)
	if err != nil {
		return 0, err
	}
	next, err := c.repo.DeletePanoDocShare(ctx, docID, ch.TeamID, ch.UserID, ch.Version, c.panoActivity(d))
	return next, fromRepo(err)
}
