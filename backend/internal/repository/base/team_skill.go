// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

var (
	// ErrTeamSkillCap is a new team skill in a team workspace that holds as
	// many as it may.
	ErrTeamSkillCap = errors.New("team workspace skill cap reached")
	// ErrTeamSkillOff is a write to a team skill the workspace is opted out of.
	ErrTeamSkillOff = errors.New("team skill off in this workspace")
)

// TeamSkillRow is a team workspace's skill as one workspace, or a member,
// sees it: its team and team workspace, and whether the workspace is opted
// in (always true for a member's read on the team's page).
type TeamSkillRow struct {
	model.Skill       `gorm:"embedded"`
	TeamID            int64
	TeamWorkspaceName string
	On                bool `gorm:"column:opted_in"`
}

// WorkspaceTeamWorkspace is a team workspace a workspace is shared into.
type WorkspaceTeamWorkspace struct {
	ID       int64
	Name     string
	TeamID   int64
	TeamName string
}

// TeamSkillAccess is who reaches a team skill, and how a change is
// recorded. From a workspace (an agent, or its owner's Skills tab),
// WorkspaceID is set, the content workspace a fork reads through: it reaches
// the skills of every team workspace it is shared into while its owner is in
// that team and it is not archived, and writes only those it is opted into.
// From the team's page, UserID and TeamID are set: a member reaches every
// skill of the team's team workspaces. NewID and At make the activity rows.
type TeamSkillAccess struct {
	WorkspaceID int64
	UserID      int64
	TeamID      int64
	NewID       func() int64
	At          time.Time
}

// teamSkillOn is whether workspace_shares AS sh's workspace is opted into
// skills: its choice row's, or with none the share's auto opt-in. Two
// false arguments.
const teamSkillOn = "(CASE WHEN choice.id IS NULL THEN sh.manual_team_skills = ? ELSE choice.off = ? END)"

// teamSkillsIn selects the team skills workspaceID reaches, with sh, its
// share into their team workspace, tw that team workspace and choice its
// choice row, if any.
func teamSkillsIn(db *gorm.DB, workspaceID int64) *gorm.DB {
	return db.Model(&model.Skill{}).
		Joins("JOIN workspace_shares AS sh ON sh.team_workspace_id = skills.team_workspace_id AND sh.workspace_id = ?", workspaceID).
		Joins("JOIN workspaces AS w ON w.id = sh.workspace_id AND w.archived_at IS NULL").
		Joins("JOIN team_members AS m ON m.team_id = sh.team_id AND m.user_id = w.user_id").
		Joins("JOIN team_workspaces AS tw ON tw.id = skills.team_workspace_id").
		Joins("LEFT JOIN team_skill_choices AS choice ON choice.skill_id = skills.id AND choice.workspace_id = sh.workspace_id").
		Where("skills.team_workspace_id <> ?", 0)
}

const teamSkillColumns = "skills.*, tw.team_id, tw.name AS team_workspace_name"

// memberSkillOn is opted_in for a member's read, always true: a literal
// rather than a bound true, which Postgres cannot type in a select list.
const memberSkillOn = "(1 = 1) AS opted_in"

// teamSkillsOfMember selects the skills of the team's team workspaces, while
// userID is in the team.
func teamSkillsOfMember(db *gorm.DB, userID, teamID int64) *gorm.DB {
	return db.Model(&model.Skill{}).
		Joins("JOIN team_workspaces AS tw ON tw.id = skills.team_workspace_id AND tw.team_id = ?", teamID).
		Joins("JOIN team_members AS m ON m.team_id = tw.team_id AND m.user_id = ?", userID)
}

// ListWorkspaceTeamSkills lists the team skills a workspace reaches, by
// name, and whether it is opted into each; with onlyOn, only those, as its
// agent sees them. q, given, keeps those whose name or description holds it.
func (r *repository) ListWorkspaceTeamSkills(ctx context.Context, workspaceID int64, q string, onlyOn bool) ([]TeamSkillRow, error) {
	query := teamSkillsIn(r.conn(ctx), workspaceID).
		Select(teamSkillColumns+", "+teamSkillOn+" AS opted_in", false, false)
	if onlyOn {
		query = query.Where(teamSkillOn, false, false)
	}
	if q != "" {
		pattern := "%" + likeEscaper.Replace(strings.ToLower(q)) + "%"
		query = query.Where(`(LOWER(skills.name) LIKE ? ESCAPE '\' OR LOWER(skills.description) LIKE ? ESCAPE '\')`, pattern, pattern)
	}
	rows := []TeamSkillRow{}
	err := query.Order("skills.name ASC, tw.name ASC, skills.id ASC").Scan(&rows).Error
	return rows, err
}

// GetWorkspaceTeamSkill is the team skill of that name a workspace reaches,
// opted in or not; ErrNotFound when it reaches none. Shared into two team
// workspaces that both have one, the older team workspace's.
func (r *repository) GetWorkspaceTeamSkill(ctx context.Context, workspaceID int64, name string) (TeamSkillRow, error) {
	return first[TeamSkillRow](teamSkillsIn(r.conn(ctx), workspaceID).
		Select(teamSkillColumns+", "+teamSkillOn+" AS opted_in", false, false).
		Where("skills.name = ?", name).
		Order("tw.id ASC"))
}

// ListWorkspaceTeamWorkspaces is the team workspaces a workspace is shared
// into while its owner is in their team and it is not archived, by name.
func (r *repository) ListWorkspaceTeamWorkspaces(ctx context.Context, workspaceID int64) ([]WorkspaceTeamWorkspace, error) {
	rows := []WorkspaceTeamWorkspace{}
	err := r.conn(ctx).Table("workspace_shares AS sh").
		Select("tw.id, tw.name, tw.team_id, teams.name AS team_name").
		Joins("JOIN workspaces AS w ON w.id = sh.workspace_id AND w.archived_at IS NULL").
		Joins("JOIN team_members AS m ON m.team_id = sh.team_id AND m.user_id = w.user_id").
		Joins("JOIN team_workspaces AS tw ON tw.id = sh.team_workspace_id").
		Joins("JOIN teams ON teams.id = tw.team_id").
		Where("sh.workspace_id = ?", workspaceID).
		Order("tw.name ASC, tw.id ASC").
		Scan(&rows).Error
	return rows, err
}

// ListTeamWorkspaceSkills is a team workspace's skills, by name, for a
// member of its team; ErrNotFound for anyone else or a team workspace of
// another team.
func (r *repository) ListTeamWorkspaceSkills(ctx context.Context, userID, teamID, teamWorkspaceID int64) ([]TeamSkillRow, error) {
	if _, err := memberTeamWorkspace(r.conn(ctx), userID, teamID, teamWorkspaceID); err != nil {
		return nil, err
	}
	rows := []TeamSkillRow{}
	err := teamSkillsOfMember(r.conn(ctx), userID, teamID).
		Select(teamSkillColumns+", "+memberSkillOn).
		Where("skills.team_workspace_id = ?", teamWorkspaceID).
		Order("skills.name ASC").
		Scan(&rows).Error
	return rows, err
}

// GetTeamSkill is one of the team's skills, by id, for a member.
func (r *repository) GetTeamSkill(ctx context.Context, userID, teamID, skillID int64) (TeamSkillRow, error) {
	return first[TeamSkillRow](teamSkillsOfMember(r.conn(ctx), userID, teamID).
		Select(teamSkillColumns+", "+memberSkillOn).
		Where("skills.id = ?", skillID))
}

// memberTeamWorkspace is the team's team workspace, while userID is in the
// team.
func memberTeamWorkspace(tx *gorm.DB, userID, teamID, teamWorkspaceID int64) (model.TeamWorkspace, error) {
	return first[model.TeamWorkspace](tx.Model(&model.TeamWorkspace{}).
		Joins("JOIN team_members AS m ON m.team_id = team_workspaces.team_id AND m.user_id = ?", userID).
		Where("team_workspaces.id = ? AND team_workspaces.team_id = ?", teamWorkspaceID, teamID))
}

// teamSkillTarget is the team workspace a write lands in and who makes it,
// checked inside the write's transaction.
type teamSkillTarget struct {
	TeamWorkspace model.TeamWorkspace
	ActorID       int64
}

// reachTeamWorkspace checks that access reaches a team workspace, and
// returns it with the actor to record: the workspace's owner, or the member.
func reachTeamWorkspace(tx *gorm.DB, a TeamSkillAccess, teamWorkspaceID int64) (teamSkillTarget, error) {
	if a.WorkspaceID == 0 {
		tw, err := memberTeamWorkspace(tx, a.UserID, a.TeamID, teamWorkspaceID)
		return teamSkillTarget{TeamWorkspace: tw, ActorID: a.UserID}, err
	}
	var row struct {
		model.TeamWorkspace `gorm:"embedded"`
		OwnerID             int64
	}
	err := tx.Table("workspace_shares AS sh").
		Select("tw.*, w.user_id AS owner_id").
		Joins("JOIN workspaces AS w ON w.id = sh.workspace_id AND w.archived_at IS NULL").
		Joins("JOIN team_members AS m ON m.team_id = sh.team_id AND m.user_id = w.user_id").
		Joins("JOIN team_workspaces AS tw ON tw.id = sh.team_workspace_id").
		Where("sh.workspace_id = ? AND sh.team_workspace_id = ?", a.WorkspaceID, teamWorkspaceID).
		Limit(1).Scan(&row).Error
	if err != nil {
		return teamSkillTarget{}, err
	}
	if row.ID == 0 {
		return teamSkillTarget{}, ErrNotFound
	}
	return teamSkillTarget{TeamWorkspace: row.TeamWorkspace, ActorID: row.OwnerID}, nil
}

// reachTeamSkill checks that access may change the team skill, and returns
// it with its target: from a workspace, only while it is opted in
// (ErrTeamSkillOff when it reaches it but is not).
func reachTeamSkill(tx *gorm.DB, a TeamSkillAccess, skillID int64) (model.Skill, teamSkillTarget, error) {
	s, err := first[model.Skill](tx.Where("id = ? AND team_workspace_id <> ?", skillID, 0))
	if err != nil {
		return model.Skill{}, teamSkillTarget{}, err
	}
	t, err := reachTeamWorkspace(tx, a, s.TeamWorkspaceID)
	if err != nil {
		return model.Skill{}, teamSkillTarget{}, err
	}
	if a.WorkspaceID != 0 {
		var n int64
		if err := teamSkillsIn(tx, a.WorkspaceID).Where("skills.id = ?", skillID).Where(teamSkillOn, false, false).Count(&n).Error; err != nil {
			return model.Skill{}, teamSkillTarget{}, err
		}
		if n == 0 {
			return model.Skill{}, teamSkillTarget{}, ErrTeamSkillOff
		}
	}
	return s, t, nil
}

// lockTeamWorkspace holds the team workspace's row until the transaction
// ends, so two new skills counted against its cap run one after the other.
func lockTeamWorkspace(tx *gorm.DB, id int64, at time.Time) error {
	res := tx.Exec("UPDATE team_workspaces SET version = version + 1, updated_at = ? WHERE id = ?", at, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// underTeamSkillCap refuses one more skill in a team workspace that has
// maxSkills; 0 is no cap.
func underTeamSkillCap(tx *gorm.DB, teamWorkspaceID int64, maxSkills int) error {
	if maxSkills <= 0 {
		return nil
	}
	var n int64
	if err := tx.Model(&model.Skill{}).Where("team_workspace_id = ?", teamWorkspaceID).Count(&n).Error; err != nil {
		return err
	}
	if n >= int64(maxSkills) {
		return ErrTeamSkillCap
	}
	return nil
}

// recordTeamSkill writes the activity of a change to a team skill.
func recordTeamSkill(tx *gorm.DB, a TeamSkillAccess, t teamSkillTarget, action, name string) error {
	act := model.TeamManagementActivity{
		ID: a.NewID(), TeamID: t.TeamWorkspace.TeamID, ActorUserID: t.ActorID, Action: action, CreatedAt: a.At,
		Details: TeamActivityDetails(map[string]string{"skill": name, "teamWorkspace": t.TeamWorkspace.Name}),
	}
	if a.WorkspaceID != 0 {
		act.TargetWorkspaceID = &a.WorkspaceID
	}
	return record(tx, act)
}

// CreateTeamSkill stores a new skill of a team workspace with its first
// files, after checking access reaches the team workspace and it is under
// maxSkills, and records it. It returns the team id. A name the team
// workspace already has is gorm.ErrDuplicatedKey.
func (r *repository) CreateTeamSkill(ctx context.Context, a TeamSkillAccess, s model.Skill, files []model.SkillFile, maxSkills int) (model.Skill, int64, error) {
	var teamID int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTeamWorkspace(tx, s.TeamWorkspaceID, a.At); err != nil {
			return err
		}
		t, err := reachTeamWorkspace(tx, a, s.TeamWorkspaceID)
		if err != nil {
			return err
		}
		if err := underTeamSkillCap(tx, s.TeamWorkspaceID, maxSkills); err != nil {
			return err
		}
		if err := tx.Create(&s).Error; err != nil {
			return err
		}
		if _, err := replaceSkill(tx, &s, files, nil); err != nil {
			return err
		}
		teamID = t.TeamWorkspace.TeamID
		return recordTeamSkill(tx, a, t, model.TeamActivitySkillCreated, s.Name)
	})
	if err != nil {
		return model.Skill{}, 0, err
	}
	return s, teamID, nil
}

// ReplaceTeamSkill writes exactly the given files to an existing team
// skill, as a re-import does, after checking access may change it. It
// returns the skill and the storage ids of the files it dropped.
func (r *repository) ReplaceTeamSkill(ctx context.Context, a TeamSkillAccess, s model.Skill, files []model.SkillFile) (model.Skill, []string, error) {
	var dropped []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		cur, _, err := reachTeamSkill(tx, a, s.ID)
		if err != nil {
			return err
		}
		s.UserID, s.WorkspaceID, s.TeamWorkspaceID, s.CreatedAt = 0, 0, cur.TeamWorkspaceID, cur.CreatedAt
		dropped, err = replaceSkill(tx, &s, files, nil)
		return err
	})
	if err != nil {
		return model.Skill{}, nil, err
	}
	return s, dropped, nil
}

// UpsertTeamSkillFile writes one file of an existing team skill, after
// checking access may change it, on expect as UpsertSkillFile does. It
// returns the skill, the file as stored and the storage id it replaced.
func (r *repository) UpsertTeamSkillFile(ctx context.Context, a TeamSkillAccess, s model.Skill, f model.SkillFile, expect *int64) (model.Skill, model.SkillFile, string, error) {
	var replaced string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		cur, _, err := reachTeamSkill(tx, a, s.ID)
		if err != nil {
			return err
		}
		s.UserID, s.WorkspaceID, s.TeamWorkspaceID = 0, 0, cur.TeamWorkspaceID
		replaced, err = upsertSkillFile(tx, &s, &f, nil, expect)
		return err
	})
	if err != nil {
		return model.Skill{}, model.SkillFile{}, "", err
	}
	return s, f, replaced, nil
}

// DeleteTeamSkillFile removes one file of a team skill access may change.
func (r *repository) DeleteTeamSkillFile(ctx context.Context, a TeamSkillAccess, s model.Skill, path string) (model.Skill, string, error) {
	var storageID string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		cur, _, err := reachTeamSkill(tx, a, s.ID)
		if err != nil {
			return err
		}
		s.UserID, s.WorkspaceID, s.TeamWorkspaceID = 0, 0, cur.TeamWorkspaceID
		storageID, err = deleteSkillFile(tx, &s, path)
		return err
	})
	if err != nil {
		return model.Skill{}, "", err
	}
	return s, storageID, nil
}

// DeleteTeamSkill deletes a team skill for every workspace, as a member of
// its team, and records it. It returns the team id and the storage ids to
// purge after the commit.
func (r *repository) DeleteTeamSkill(ctx context.Context, a TeamSkillAccess, skillID int64) (int64, []string, error) {
	var teamID int64
	var storageIDs []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		s, t, err := reachTeamSkill(tx, a, skillID)
		if err != nil {
			return err
		}
		if storageIDs, err = deleteSkill(tx, skillID); err != nil {
			return err
		}
		teamID = t.TeamWorkspace.TeamID
		return recordTeamSkill(tx, a, t, model.TeamActivitySkillDeleted, s.Name)
	})
	return teamID, storageIDs, err
}

// setChoice stores a workspace's choice of a team skill, replacing any.
func setChoice(tx *gorm.DB, c model.TeamSkillChoice) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "skill_id"}, {Name: "workspace_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"off", "user_id"}),
	}).Create(&c).Error
}

// SetTeamSkillChoice opts a workspace into, or out of, a team skill it
// reaches, as its owner chose, and records it. It returns the team id.
func (r *repository) SetTeamSkillChoice(ctx context.Context, a TeamSkillAccess, c model.TeamSkillChoice) (int64, error) {
	var teamID int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		s, err := first[model.Skill](tx.Where("id = ? AND team_workspace_id <> ?", c.SkillID, 0))
		if err != nil {
			return err
		}
		t, err := reachTeamWorkspace(tx, a, s.TeamWorkspaceID)
		if err != nil {
			return err
		}
		c.WorkspaceID = a.WorkspaceID
		if err := setChoice(tx, c); err != nil {
			return err
		}
		teamID = t.TeamWorkspace.TeamID
		action := model.TeamActivitySkillOptedIn
		if c.Off {
			action = model.TeamActivitySkillOptedOut
		}
		return recordTeamSkill(tx, a, t, action, s.Name)
	})
	return teamID, err
}

// OptOutTeamSkill is an agent deleting a team skill its workspace is opted
// into: the workspace is opted out, and the skill is deleted when no
// workspace is opted in any more (one with auto opt-in on and no choice
// counts as opted in). It returns the team id, whether the skill was
// deleted, and the storage ids to purge after the commit.
func (r *repository) OptOutTeamSkill(ctx context.Context, a TeamSkillAccess, c model.TeamSkillChoice) (int64, bool, []string, error) {
	var teamID int64
	var gone bool
	var storageIDs []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		s, t, err := reachTeamSkill(tx, a, c.SkillID)
		if err != nil {
			return err
		}
		teamID = t.TeamWorkspace.TeamID
		c.WorkspaceID, c.Off = a.WorkspaceID, true
		if err := setChoice(tx, c); err != nil {
			return err
		}
		if err := recordTeamSkill(tx, a, t, model.TeamActivitySkillOptedOut, s.Name); err != nil {
			return err
		}
		var n int64
		if err := tx.Table("workspace_shares AS sh").
			Joins("JOIN workspaces AS w ON w.id = sh.workspace_id").
			Joins("JOIN team_members AS m ON m.team_id = sh.team_id AND m.user_id = w.user_id").
			Joins("LEFT JOIN team_skill_choices AS choice ON choice.skill_id = ? AND choice.workspace_id = sh.workspace_id", s.ID).
			Where("sh.team_workspace_id = ?", s.TeamWorkspaceID).
			Where(teamSkillOn, false, false).
			Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		if storageIDs, err = deleteSkill(tx, s.ID); err != nil {
			return err
		}
		gone = true
		return recordTeamSkill(tx, a, t, model.TeamActivitySkillDeleted, s.Name)
	})
	return teamID, gone, storageIDs, err
}

// GiveSkillToTeam moves userID's own skill into a team workspace its
// workspace (the content workspace, for a fork) is shared into, keeping its
// id: it belongs to no person from then on, and is on wherever the team
// workspace's shares opt in. files are its files with their content copied
// to the team workspace's keys. It is refused as ErrNotFound for a skill
// not the account's own or a team workspace the workspace does not reach,
// ErrTeamSkillCap over maxSkills, and gorm.ErrDuplicatedKey when the team
// workspace has a skill of its name. It returns the team id and the storage
// ids the copies replaced, to purge after the commit.
func (r *repository) GiveSkillToTeam(ctx context.Context, a TeamSkillAccess, userID, skillID, teamWorkspaceID int64, files []model.SkillFile, maxSkills int) (int64, []string, error) {
	var teamID int64
	var replaced []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockTeamWorkspace(tx, teamWorkspaceID, a.At); err != nil {
			return err
		}
		t, err := reachTeamWorkspace(tx, a, teamWorkspaceID)
		if err != nil {
			return err
		}
		if err := underTeamSkillCap(tx, teamWorkspaceID, maxSkills); err != nil {
			return err
		}
		s, err := first[model.Skill](tx.Where("id = ? AND user_id = ? AND workspace_id = ? AND team_workspace_id = ? AND linked_skill_id = ?",
			skillID, userID, accountSkill, 0, 0))
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Skill{}).Where("id = ?", s.ID).
			Updates(map[string]any{"user_id": 0, "team_workspace_id": teamWorkspaceID, "disabled": false}).Error; err != nil {
			return err
		}
		for _, f := range files {
			var old []string
			if err := tx.Model(&model.SkillFile{}).Where("id = ? AND skill_id = ?", f.ID, s.ID).Pluck("storage_id", &old).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.SkillFile{}).Where("id = ? AND skill_id = ?", f.ID, s.ID).UpdateColumn("storage_id", f.StorageID).Error; err != nil {
				return err
			}
			replaced = append(replaced, old...)
		}
		if err := tx.Where("skill_id = ?", s.ID).Delete(&model.WorkspaceSkill{}).Error; err != nil {
			return err
		}
		teamID = t.TeamWorkspace.TeamID
		return recordTeamSkill(tx, a, t, model.TeamActivitySkillGiven, s.Name)
	})
	if err != nil {
		return 0, nil, err
	}
	return teamID, replaced, nil
}

// optInTeamSkillsOnNow keeps a workspace's team skills on when its share's
// auto opt-in is turned off: every skill of the team workspace it is on for
// with no choice gets one, on, so only later skills start off.
func optInTeamSkillsOnNow(tx *gorm.DB, s model.WorkspaceShare, newID func() int64, at time.Time) error {
	var ids []int64
	if err := tx.Model(&model.Skill{}).
		Where("team_workspace_id = ?", s.TeamWorkspaceID).
		Where("NOT EXISTS (SELECT 1 FROM team_skill_choices AS choice WHERE choice.skill_id = skills.id AND choice.workspace_id = ?)", s.WorkspaceID).
		Order("id ASC").Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := setChoice(tx, model.TeamSkillChoice{ID: newID(), CreatedAt: at, SkillID: id, WorkspaceID: s.WorkspaceID, UserID: s.UserID}); err != nil {
			return err
		}
	}
	return nil
}
