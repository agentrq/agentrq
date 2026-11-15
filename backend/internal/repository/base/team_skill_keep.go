// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"maps"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// ErrTeamSkillsChanged is a copy of the team skills a person is losing that
// no longer matches them, or their own skills' names: plan it again.
var ErrTeamSkillsChanged = errors.New("the team skills to keep changed")

// TeamSkillsLeaving names the shares about to end: every one of a team's,
// with UserID one member's, with WorkspaceID one workspace's.
type TeamSkillsLeaving struct {
	TeamID      int64
	UserID      int64
	WorkspaceID int64
}

// TeamSkillToKeep is a team skill a workspace whose share is ending is opted
// into: its owner keeps a copy, on in that workspace.
type TeamSkillToKeep struct {
	model.Skill       `gorm:"embedded"`
	TeamWorkspaceName string
	KeepWorkspaceID   int64
	KeepUserID        int64
}

// KeptTeamSkill is one workspace's use of one team skill.
type KeptTeamSkill struct {
	WorkspaceID int64
	SkillID     int64
}

// OwnSkillName is one of an account's skill names.
type OwnSkillName struct {
	UserID int64
	Name   string
}

// TeamSkillCopies is what the people losing team skills keep, and what it
// was planned from, which the change checks again under the team's lock.
// The zero value keeps nothing, and fits only when nothing is to be kept.
type TeamSkillCopies struct {
	// Kept is every team skill kept, by workspace, with its SkillFileSums.
	Kept map[KeptTeamSkill]string
	// Names is every account skill name the plan relied on, with its
	// skill's OwnSkillSums, or "" for a name the account did not have.
	Names map[OwnSkillName]string
	// Skills and Files are the new account skills, their blobs written.
	Skills []model.Skill
	Files  []model.SkillFile
	// On turns each kept skill, new or the account's own, on in its workspace.
	On []model.WorkspaceSkill
}

// SkillFileSums is a skill's files by path and content, in one string: two
// skills are the same when theirs are equal.
func SkillFileSums(files []model.SkillFile) string {
	lines := make([]string, len(files))
	for i, f := range files {
		lines[i] = f.Path + "\x00" + f.SHA256
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// OwnSkillSums is an account skill's SkillFileSums, told apart from a name
// the account does not have; a link to a teammate's skill has no files, and
// so is never the same as a team skill.
func OwnSkillSums(s model.Skill, files []model.SkillFile) string {
	if s.LinkedSkillID != 0 {
		return "link"
	}
	return "+" + SkillFileSums(files)
}

// teamSkillsLeaving selects the team skills the workspaces of the ending
// shares are opted into, while their owners are in the team and they are
// not archived.
func teamSkillsLeaving(db *gorm.DB, sel TeamSkillsLeaving) *gorm.DB {
	q := db.Table("workspace_shares AS sh").
		Select("skills.*, tw.name AS team_workspace_name, sh.workspace_id AS keep_workspace_id, w.user_id AS keep_user_id").
		Joins("JOIN workspaces AS w ON w.id = sh.workspace_id AND w.archived_at IS NULL").
		Joins("JOIN team_members AS m ON m.team_id = sh.team_id AND m.user_id = w.user_id").
		Joins("JOIN team_workspaces AS tw ON tw.id = sh.team_workspace_id").
		Joins("JOIN skills ON skills.team_workspace_id = sh.team_workspace_id").
		Joins("LEFT JOIN team_skill_choices AS choice ON choice.skill_id = skills.id AND choice.workspace_id = sh.workspace_id").
		Where("sh.team_id = ?", sel.TeamID).
		Where(teamSkillOn, false, false)
	if sel.UserID != 0 {
		q = q.Where("sh.user_id = ?", sel.UserID)
	}
	if sel.WorkspaceID != 0 {
		q = q.Where("sh.workspace_id = ?", sel.WorkspaceID)
	}
	return q.Order("w.user_id ASC, skills.name ASC, tw.id ASC, sh.workspace_id ASC")
}

// ListTeamSkillsToKeep lists the team skills the people losing them keep:
// every one a workspace of the ending shares is opted into.
func (r *repository) ListTeamSkillsToKeep(ctx context.Context, sel TeamSkillsLeaving) ([]TeamSkillToKeep, error) {
	rows := []TeamSkillToKeep{}
	err := teamSkillsLeaving(r.conn(ctx), sel).Scan(&rows).Error
	return rows, err
}

// fileSumsOf is each skill's SkillFileSums.
func fileSumsOf(tx *gorm.DB, skillIDs []int64) (map[int64]string, error) {
	var files []model.SkillFile
	if err := tx.Where("skill_id IN ?", skillIDs).Find(&files).Error; err != nil {
		return nil, err
	}
	bySkill := map[int64][]model.SkillFile{}
	for _, f := range files {
		bySkill[f.SkillID] = append(bySkill[f.SkillID], f)
	}
	out := make(map[int64]string, len(skillIDs))
	for _, id := range skillIDs {
		out[id] = SkillFileSums(bySkill[id])
	}
	return out, nil
}

// keptNow is the team skills the ending shares' workspaces are opted into
// now, as TeamSkillCopies.Kept.
func keptNow(tx *gorm.DB, sel TeamSkillsLeaving) (map[KeptTeamSkill]string, error) {
	var rows []TeamSkillToKeep
	if err := teamSkillsLeaving(tx, sel).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[KeptTeamSkill]string, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int64, len(rows))
	for i, s := range rows {
		ids[i] = s.ID
	}
	sums, err := fileSumsOf(tx, ids)
	if err != nil {
		return nil, err
	}
	for _, s := range rows {
		out[KeptTeamSkill{WorkspaceID: s.KeepWorkspaceID, SkillID: s.ID}] = sums[s.ID]
	}
	return out, nil
}

// namesNow is each of the names as TeamSkillCopies.Names reads now.
func namesNow(tx *gorm.DB, names map[OwnSkillName]string) (map[OwnSkillName]string, error) {
	out := make(map[OwnSkillName]string, len(names))
	for n := range names {
		var s model.Skill
		err := tx.Where("user_id = ? AND workspace_id = ? AND team_workspace_id = ? AND name = ?", n.UserID, accountSkill, 0, n.Name).Take(&s).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			out[n] = ""
			continue
		}
		if err != nil {
			return nil, err
		}
		var files []model.SkillFile
		if err := tx.Where("skill_id = ?", s.ID).Find(&files).Error; err != nil {
			return nil, err
		}
		out[n] = OwnSkillSums(s, files)
	}
	return out, nil
}

// keepTeamSkills gives the people losing the ending shares' team skills
// their copies, once the skills and names are still what the copies were
// planned from. It holds the team's team workspaces first, so no file of
// their skills changes before the copies commit.
func keepTeamSkills(tx *gorm.DB, sel TeamSkillsLeaving, c TeamSkillCopies, at time.Time) error {
	if err := tx.Exec("UPDATE team_workspaces SET version = version + 1, updated_at = ? WHERE team_id = ?", at, sel.TeamID).Error; err != nil {
		return err
	}
	kept, err := keptNow(tx, sel)
	if err != nil {
		return err
	}
	names, err := namesNow(tx, c.Names)
	if err != nil {
		return err
	}
	if !maps.Equal(kept, c.Kept) || !maps.Equal(names, c.Names) {
		return ErrTeamSkillsChanged
	}
	if len(c.Skills) > 0 {
		if err := tx.Create(&c.Skills).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrTeamSkillsChanged
			}
			return err
		}
	}
	if len(c.Files) > 0 {
		if err := tx.Create(&c.Files).Error; err != nil {
			return err
		}
	}
	if len(c.On) > 0 {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&c.On).Error
	}
	return nil
}

// teamSkillsOfTeam selects the ids of the team's team workspaces' skills.
func teamSkillsOfTeam(tx *gorm.DB, teamID int64) *gorm.DB {
	return tx.Model(&model.Skill{}).Select("id").
		Where("team_workspace_id IN (?)", tx.Model(&model.TeamWorkspace{}).Select("id").Where("team_id = ?", teamID))
}

// dropTeamSkillChoices deletes the workspaces' choices of the team's skills,
// once their shares into it are gone.
func dropTeamSkillChoices(tx *gorm.DB, teamID int64, workspaceIDs []int64) error {
	if len(workspaceIDs) == 0 {
		return nil
	}
	return tx.Where("workspace_id IN ? AND skill_id IN (?)", workspaceIDs, teamSkillsOfTeam(tx, teamID)).
		Delete(&model.TeamSkillChoice{}).Error
}

// deleteTeamSkills deletes the team's skills, their files and choices, and
// returns the blobs to purge once the change commits.
func deleteTeamSkills(tx *gorm.DB, teamID int64) ([]string, error) {
	var blobs []string
	if err := tx.Model(&model.SkillFile{}).Where("skill_id IN (?)", teamSkillsOfTeam(tx, teamID)).Pluck("storage_id", &blobs).Error; err != nil {
		return nil, err
	}
	for _, m := range []any{&model.TeamSkillChoice{}, &model.SkillFile{}} {
		if err := tx.Where("skill_id IN (?)", teamSkillsOfTeam(tx, teamID)).Delete(m).Error; err != nil {
			return nil, err
		}
	}
	err := tx.Where("team_workspace_id IN (?)", tx.Model(&model.TeamWorkspace{}).Select("id").Where("team_id = ?", teamID)).
		Delete(&model.Skill{}).Error
	return blobs, err
}
