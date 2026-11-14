// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// PanoDocShareWithTeam is a share of a Pano doc with its team's name.
type PanoDocShareWithTeam struct {
	model.PanoDocShare
	TeamName string
}

// TeamPanoDocRow is a Pano doc shared with a team, without its markdown,
// with its owner.
type TeamPanoDocRow struct {
	model.PanoDoc
	OwnerName    string
	OwnerPicture string
}

// panoSharesWithTeams selects Pano doc shares with their team's name.
func panoSharesWithTeams(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.PanoDocShare{}).
		Select("pano_doc_shares.id, pano_doc_shares.created_at, pano_doc_shares.team_id, " +
			"pano_doc_shares.doc_id, pano_doc_shares.user_id, teams.name AS team_name").
		Joins("JOIN teams ON teams.id = pano_doc_shares.team_id")
}

// SetPanoDocShare shares the owner's Pano doc with a team from version, and
// returns the share and the new version. The sharer's membership is read
// under the team's lock, as for a workspace; ErrNotFound when the team is
// gone, the sharer is not in it, or the doc is not theirs or has expired.
// Sharing a doc already shared changes nothing and records nothing.
func (r *repository) SetPanoDocShare(ctx context.Context, s model.PanoDocShare, version int64, act model.TeamManagementActivity) (PanoDocShareWithTeam, int64, error) {
	var out PanoDocShareWithTeam
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, s.TeamID, s.UserID, version, s.CreatedAt); err != nil {
			return err
		}
		if err := panoDocExists(tx, s.UserID, s.DocID, s.CreatedAt); err != nil {
			return err
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&s)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			act.TeamID, act.ActorUserID, act.Action = s.TeamID, s.UserID, model.TeamActivityPanoShared
			if err := record(tx, act); err != nil {
				return err
			}
		}
		out, err = first[PanoDocShareWithTeam](panoSharesWithTeams(tx).Where("pano_doc_shares.team_id = ? AND pano_doc_shares.doc_id = ?", s.TeamID, s.DocID))
		return err
	})
	return out, next, err
}

// ListPanoDocShares returns the owner's shares of a Pano doc, by team name.
func (r *repository) ListPanoDocShares(ctx context.Context, docID, userID int64) ([]PanoDocShareWithTeam, error) {
	rows := []PanoDocShareWithTeam{}
	err := panoSharesWithTeams(r.conn(ctx)).
		Where("pano_doc_shares.doc_id = ? AND pano_doc_shares.user_id = ?", docID, userID).
		Order("teams.name ASC, teams.id ASC").
		Scan(&rows).Error
	return rows, err
}

// DeletePanoDocShare unshares the owner's Pano doc from a team, from
// version, and returns the new version; ErrNotFound when the owner is not in
// the team or it was not shared there.
func (r *repository) DeletePanoDocShare(ctx context.Context, docID, teamID, userID, version int64, act model.TeamManagementActivity) (int64, error) {
	var next int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if next, err = changeTeam(tx, teamID, userID, version, act.CreatedAt); err != nil {
			return err
		}
		res := tx.Where("doc_id = ? AND team_id = ? AND user_id = ?", docID, teamID, userID).Delete(&model.PanoDocShare{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		act.TeamID, act.ActorUserID, act.Action = teamID, userID, model.TeamActivityPanoUnshared
		return record(tx, act)
	})
	return next, err
}

// ListTeamPanoDocs returns the Pano docs shared with a team that have not
// expired by now, without their markdown, the most recently updated first.
func (r *repository) ListTeamPanoDocs(ctx context.Context, teamID int64, now time.Time) ([]TeamPanoDocRow, error) {
	rows := []TeamPanoDocRow{}
	err := r.conn(ctx).Model(&model.PanoDoc{}).
		Select("pano_docs.id, pano_docs.created_at, pano_docs.updated_at, pano_docs.user_id, pano_docs.title, pano_docs.excerpt, "+
			"pano_docs.size_bytes, pano_docs.created_by_workspace_id, pano_docs.updated_by_workspace_id, pano_docs.ttl_days, "+
			"pano_docs.expires_at, pano_docs.color, pano_docs.checklist_done, pano_docs.checklist_total, pano_docs.version, "+
			"COALESCE(users.name, '') AS owner_name, COALESCE(users.picture, '') AS owner_picture").
		Joins("JOIN pano_doc_shares ON pano_doc_shares.doc_id = pano_docs.id AND pano_doc_shares.team_id = ?", teamID).
		Joins("LEFT JOIN users ON users.id = pano_docs.user_id").
		Where("pano_docs.expires_at > ?", now).
		Order("pano_docs.updated_at DESC, pano_docs.id DESC").
		Scan(&rows).Error
	return rows, err
}

// PanoDocOwner is the owner of a Pano doc a teammate reads.
type PanoDocOwner struct {
	UserID  int64
	Name    string
	Picture string
}

// PanoDocOwnerInTeam is the owner of a Pano doc the user reads in the team:
// one shared with it while the user is in it. ErrNotFound when not.
func (r *repository) PanoDocOwnerInTeam(ctx context.Context, userID, teamID, docID int64) (PanoDocOwner, error) {
	rows := []PanoDocOwner{}
	err := r.conn(ctx).Model(&model.PanoDocShare{}).
		Select("pano_doc_shares.user_id, COALESCE(users.name, '') AS name, COALESCE(users.picture, '') AS picture").
		Joins("JOIN team_members ON team_members.team_id = pano_doc_shares.team_id AND team_members.user_id = ?", userID).
		Joins("LEFT JOIN users ON users.id = pano_doc_shares.user_id").
		Where("pano_doc_shares.team_id = ? AND pano_doc_shares.doc_id = ?", teamID, docID).
		Limit(1).Scan(&rows).Error
	if err != nil {
		return PanoDocOwner{}, err
	}
	if len(rows) == 0 {
		return PanoDocOwner{}, ErrNotFound
	}
	return rows[0], nil
}
