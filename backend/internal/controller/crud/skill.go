// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/skill"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
	"gorm.io/gorm"
)

// SkillController manages an account's skills, and which of its workspaces
// each one is on in.
//
// Every write goes through here — the REST API, the MCP tools and the GitHub
// importer alike — so the rules in service/skill hold whichever door a skill
// came in by. A request with a WorkspaceID is made from that workspace (its
// parent, for a fork); one without is made from the account.
type SkillController interface {
	SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error)
	GetSkill(ctx context.Context, req entity.GetSkillRequest) (*entity.GetSkillResponse, error)
	SetSkillEnabled(ctx context.Context, req entity.SetSkillEnabledRequest) (*entity.SetSkillEnabledResponse, error)
	GetSkillFile(ctx context.Context, req entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error)
	SaveSkillFile(ctx context.Context, req entity.SaveSkillFileRequest) (*entity.SaveSkillFileResponse, error)
	DeleteSkill(ctx context.Context, req entity.DeleteSkillRequest) error
	DeleteSkillFile(ctx context.Context, req entity.DeleteSkillFileRequest) (*entity.DeleteSkillFileResponse, error)
	ImportSkills(ctx context.Context, req entity.ImportSkillsRequest) (*entity.ImportSkillsResponse, error)
	MoveLegacySkills(ctx context.Context) error
}

const (
	skillSourceManual = "manual"
	skillSourceGitHub = "github"
)

// SkillErrorKind says what sort of refusal a SkillError is, which is what the
// transports turn into a status code.
type SkillErrorKind int

const (
	// SkillInvalid is input that breaks a rule; the message says which.
	SkillInvalid SkillErrorKind = iota + 1
	// SkillConflict is a name that is already taken.
	SkillConflict
	// SkillUpstream is GitHub failing to serve an import.
	SkillUpstream
)

// SkillError is a refusal whose message is written for the caller — a person
// in the settings screen or an agent over MCP — and is safe to show them.
type SkillError struct {
	Kind    SkillErrorKind
	Message string
}

func (e *SkillError) Error() string { return e.Message }

func skillErr(kind SkillErrorKind, format string, args ...any) error {
	return &SkillError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// skillScope resolves the account a skill request is made for, and the
// workspace it is made from: 0 for the account itself, or the workspace whose
// skills workspaceID uses, which is the parent for a fork. A workspace the
// caller does not own is not found.
func (c *controller) skillScope(ctx context.Context, workspaceID int64, userID string) (uid, scope int64, err error) {
	if workspaceID != 0 {
		return c.memoryOwner(ctx, workspaceID, userID)
	}
	uid = monoflake.IDFromBase62(userID).Int64()
	if uid == 0 {
		return 0, 0, fmt.Errorf("invalid request")
	}
	return uid, 0, nil
}

// skillWithWorkspaces is s as the caller sees it, with the workspaces it is on
// in, and whether one of them is scope.
func (c *controller) skillWithWorkspaces(ctx context.Context, s model.Skill, scope int64) (entity.Skill, error) {
	on, err := c.repository.ListSkillWorkspaces(ctx, []int64{s.ID})
	if err != nil {
		return entity.Skill{}, err
	}
	return fromModelSkill(s, on[s.ID], scope), nil
}

func skillName(name string) (string, error) {
	canonical, err := skill.CanonicalName(name)
	if err != nil {
		return "", skillErr(SkillInvalid, "%v", err)
	}
	return canonical, nil
}

const (
	// MaxSkillSearchLimit is the largest page a search returns.
	MaxSkillSearchLimit = 100
	// MinSkillQueryLength and MaxSkillQueryLength bound a search query that is
	// given at all, in characters; an empty query lists every skill.
	MinSkillQueryLength = 3
	MaxSkillQueryLength = 256
)

// SearchSkills finds an account's skills whose name or description contains
// the query, ignoring case. With no query it lists them all, and with no limit
// it returns every match. EnabledOnly keeps only the skills the workspace's
// agent sees.
func (c *controller) SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error) {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	q := strings.TrimSpace(req.Query)
	switch n := utf8.RuneCountInString(q); {
	case n > 0 && n < MinSkillQueryLength:
		return nil, skillErr(SkillInvalid, "search query %q is too short; give at least %d characters, or none to list every skill", q, MinSkillQueryLength)
	case n > MaxSkillQueryLength:
		return nil, skillErr(SkillInvalid, "search query is %d characters; the limit is %d", n, MaxSkillQueryLength)
	case req.Limit < 0 || req.Offset < 0:
		return nil, skillErr(SkillInvalid, "limit and offset cannot be negative")
	}
	filter := base.SkillFilter{}
	if req.EnabledOnly {
		filter = base.SkillFilter{InWorkspace: scope, EnabledOnly: true}
	}
	limit := min(req.Limit, MaxSkillSearchLimit)
	found, total, err := c.repository.SearchSkills(ctx, uid, q, filter, limit, req.Offset)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(found))
	for i, s := range found {
		ids[i] = s.ID
	}
	on, err := c.repository.ListSkillWorkspaces(ctx, ids)
	if err != nil {
		return nil, err
	}
	skills := make([]entity.Skill, len(found))
	for i, s := range found {
		skills[i] = fromModelSkill(s, on[s.ID], scope)
	}
	c.emitSkillEvent(ctx, entity.ActionSkillSearch, uid, req.WorkspaceID, 0)
	return &entity.SearchSkillsResponse{Skills: skills, Total: int(total)}, nil
}

// emitSkillEvent counts a skill used from the interface. An MCP-origin call is
// skipped: the tool call that made it is already counted, with which tool.
func (c *controller) emitSkillEvent(ctx context.Context, action entity.Action, uid, workspaceID, skillID int64) {
	if entity.GetOrigin(ctx) == entity.OriginMCP {
		return
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		UserID:       uid,
		WorkspaceID:  workspaceID,
		ResourceType: entity.ResourceSkill,
		ResourceID:   skillID,
		Actor:        entity.ActorHuman,
	})
}

func (c *controller) GetSkill(ctx context.Context, req entity.GetSkillRequest) (*entity.GetSkillResponse, error) {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	s, err := c.repository.GetSkill(ctx, uid, name)
	if err != nil {
		return nil, err
	}
	files, err := c.repository.ListSkillFiles(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	e, err := c.skillWithWorkspaces(ctx, s, scope)
	if err != nil {
		return nil, err
	}
	e.Files = make([]entity.SkillFile, len(files))
	for i, f := range files {
		e.Files[i] = entity.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, URL: storage.PublicURL(c.skillStorage, f.StorageID)}
	}
	return &entity.GetSkillResponse{Skill: e}, nil
}

// SetSkillEnabled turns a skill on or off for agents: in one workspace when
// the request names one, or in every workspace when it does not. Off for the
// account, it stays on in its workspaces but no agent sees it until it is on
// again. Off in a workspace, that workspace's agent no longer sees it.
func (c *controller) SetSkillEnabled(ctx context.Context, req entity.SetSkillEnabledRequest) (*entity.SetSkillEnabledResponse, error) {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	s, err := c.repository.GetSkill(ctx, uid, name)
	if err != nil {
		return nil, err
	}
	on, err := c.repository.ListSkillWorkspaces(ctx, []int64{s.ID})
	if err != nil {
		return nil, err
	}
	workspaces, changed, err := c.setSkillEnabled(ctx, &s, on[s.ID], uid, scope, req.Enabled)
	if err != nil {
		return nil, err
	}
	if changed {
		action := entity.ActionSkillEnable
		if !req.Enabled {
			action = entity.ActionSkillDisable
		}
		c.emitSkillEvent(ctx, action, uid, req.WorkspaceID, s.ID)
	}
	return &entity.SetSkillEnabledResponse{Skill: fromModelSkill(s, workspaces, scope)}, nil
}

// setSkillEnabled flips the switch scope names on a skill on in workspaces,
// and returns the workspaces it is on in after, and whether the switch moved.
func (c *controller) setSkillEnabled(ctx context.Context, s *model.Skill, workspaces []int64, uid, scope int64, enabled bool) ([]int64, bool, error) {
	if scope != 0 {
		if slices.Contains(workspaces, scope) == enabled {
			return workspaces, false, nil
		}
		if !enabled {
			_, err := c.repository.TurnSkillOff(ctx, s.ID, scope)
			return slices.DeleteFunc(workspaces, func(w int64) bool { return w == scope }), true, err
		}
		err := c.repository.TurnSkillOn(ctx, model.WorkspaceSkill{ID: c.idgen.NextID(), CreatedAt: time.Now(), UserID: uid, SkillID: s.ID, WorkspaceID: scope})
		return append(workspaces, scope), true, err
	}
	if s.Disabled == !enabled {
		return workspaces, false, nil
	}
	if err := c.repository.SetSkillDisabled(ctx, s.ID, !enabled); err != nil {
		return nil, false, err
	}
	s.Disabled = !enabled
	return workspaces, true, nil
}

func (c *controller) GetSkillFile(ctx context.Context, req entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	path, err := skill.CleanPath(req.Path)
	if err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	s, err := c.repository.GetSkill(ctx, uid, name)
	if err != nil {
		return nil, err
	}
	f, err := c.repository.GetSkillFile(ctx, s.ID, path)
	if err != nil {
		return nil, err
	}
	content, err := c.skillStorage.LoadRaw(f.StorageID)
	if err != nil {
		return nil, fmt.Errorf("load skill file content: %w", err)
	}
	e, err := c.skillWithWorkspaces(ctx, s, scope)
	if err != nil {
		return nil, err
	}
	c.emitSkillEvent(ctx, entity.ActionSkillView, uid, req.WorkspaceID, s.ID)
	return &entity.GetSkillFileResponse{
		Skill: e,
		File:  entity.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, Content: string(content), URL: storage.PublicURL(c.skillStorage, f.StorageID)},
	}, nil
}

// SaveSkillFile writes one file of a skill. Saving SKILL.md creates the skill
// when there is none, on in the workspace it was saved from; any other file
// needs the skill to exist first, because a skill without a SKILL.md is
// invisible to every agent that might use it.
func (c *controller) SaveSkillFile(ctx context.Context, req entity.SaveSkillFileRequest) (*entity.SaveSkillFileResponse, error) {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	path, err := skill.CleanPath(req.Path)
	if err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	content := []byte(req.Content)

	s, err := c.repository.GetSkill(ctx, uid, name)
	exists := err == nil
	if err != nil && !errors.Is(err, base.ErrNotFound) {
		return nil, err
	}
	// What it is on in is read before anything is written, so a failure
	// reading it never reports a write that did happen as one that did not.
	var workspaces []int64
	if exists {
		on, err := c.repository.ListSkillWorkspaces(ctx, []int64{s.ID})
		if err != nil {
			return nil, err
		}
		workspaces = on[s.ID]
	}
	now := time.Now()
	var on []model.WorkspaceSkill
	if path == skill.FileName {
		fm, err := skill.ParseSkillFile(content, name)
		if err != nil {
			return nil, skillErr(SkillInvalid, "%v", err)
		}
		if fm.Name != name {
			return nil, skillErr(SkillInvalid, "%s names the skill %q, but it is being saved as %q; make the two match", skill.FileName, fm.Name, name)
		}
		if !exists {
			s = model.Skill{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, Name: name, SourceType: skillSourceManual}
			if scope != 0 {
				on = []model.WorkspaceSkill{{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, WorkspaceID: scope}}
				workspaces = []int64{scope}
			}
		}
		s.Description = fm.Description
	} else {
		if err := skill.CheckContent(path, content); err != nil {
			return nil, skillErr(SkillInvalid, "%v", err)
		}
		if !exists {
			return nil, skillErr(SkillInvalid, "skill %q does not exist yet; save its %s first", name, skill.FileName)
		}
		if _, err := c.repository.GetSkillFile(ctx, s.ID, path); errors.Is(err, base.ErrNotFound) {
			if s.FileCount >= skill.MaxFiles {
				return nil, skillErr(SkillInvalid, "skill %q already has %d files, the limit; delete one first", name, s.FileCount)
			}
		} else if err != nil {
			return nil, err
		}
	}
	if s.SourceType == skillSourceGitHub {
		s.LocallyModified = true
	}
	s.UpdatedAt = now

	f, err := c.storeSkillFile(s, path, content, now)
	if err != nil {
		return nil, err
	}
	saved, replaced, err := c.repository.UpsertSkillFile(ctx, s, f, on)
	if err != nil {
		_ = c.skillStorage.Delete(f.StorageID)
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, skillErr(SkillConflict, "skill %q was created by someone else a moment ago; try again", name)
		}
		return nil, err
	}
	if replaced != "" {
		_ = c.skillStorage.Delete(replaced)
	}
	return &entity.SaveSkillFileResponse{
		Skill: fromModelSkill(saved, workspaces, scope),
		File:  entity.SkillFile{Path: path, SizeBytes: len(content), UpdatedAt: now, URL: storage.PublicURL(c.skillStorage, f.StorageID)},
	}, nil
}

// storeSkillFile writes content to storage and returns the row describing it.
// The blob goes first so a row never points at nothing; a caller whose row
// then fails to save deletes the blob.
func (c *controller) storeSkillFile(s model.Skill, path string, content []byte, now time.Time) (model.SkillFile, error) {
	f := model.SkillFile{
		ID:        c.idgen.NextID(),
		CreatedAt: now,
		UpdatedAt: now,
		Path:      path,
	}
	if err := c.storeSkillBlob(s, &f, content); err != nil {
		return model.SkillFile{}, err
	}
	return f, nil
}

// storeSkillBlob writes content to a fresh blob of skill s and points f at it.
func (c *controller) storeSkillBlob(s model.Skill, f *model.SkillFile, content []byte) error {
	sum := sha256.Sum256(content)
	f.SizeBytes, f.SHA256 = len(content), hex.EncodeToString(sum[:])
	f.StorageID = skillStorageID(s, c.idgen.NextID())
	// Served as plain text wherever it is public, so a skill file never renders as a page.
	if _, err := storage.SaveBlob(c.skillStorage, f.StorageID, base64.StdEncoding.EncodeToString(content), skillContentType); err != nil {
		return fmt.Errorf("store skill file: %w", err)
	}
	return nil
}

const skillContentType = "text/plain; charset=utf-8"

// skillStorageID keys a skill file's blob as u-<owner>/skill-<skill>/<blob>.
// The blob id is fresh on every write, because a replace stores the new blob
// before the old one is purged.
func skillStorageID(s model.Skill, blobID int64) string {
	return "u-" + monoflake.ID(s.UserID).String() + "/skill-" + monoflake.ID(s.ID).String() + "/" + monoflake.ID(blobID).String()
}

// DeleteSkill deletes a skill from the account. Asked from a workspace, it
// turns the skill off there instead, and deletes it only when no other
// workspace has it on: an agent clearing out its own skills cannot take one
// away from another workspace.
func (c *controller) DeleteSkill(ctx context.Context, req entity.DeleteSkillRequest) error {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return err
	}
	s, err := c.repository.GetSkill(ctx, uid, name)
	if err != nil {
		return err
	}
	if scope != 0 {
		if _, err := c.repository.TurnSkillOff(ctx, s.ID, scope); err != nil {
			return err
		}
		on, err := c.repository.ListSkillWorkspaces(ctx, []int64{s.ID})
		if err != nil {
			return err
		}
		if len(on[s.ID]) > 0 {
			return nil
		}
	}
	storageIDs, err := c.repository.DeleteSkill(ctx, s.ID)
	if err != nil {
		return err
	}
	c.purge(storageIDs)
	return nil
}

func (c *controller) DeleteSkillFile(ctx context.Context, req entity.DeleteSkillFileRequest) (*entity.DeleteSkillFileResponse, error) {
	uid, scope, err := c.skillScope(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	path, err := skill.CleanPath(req.Path)
	if err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	if path == skill.FileName {
		return nil, skillErr(SkillInvalid, "a skill cannot lose its %s; delete the whole skill instead", skill.FileName)
	}
	s, err := c.repository.GetSkill(ctx, uid, name)
	if err != nil {
		return nil, err
	}
	on, err := c.repository.ListSkillWorkspaces(ctx, []int64{s.ID})
	if err != nil {
		return nil, err
	}
	if s.SourceType == skillSourceGitHub {
		s.LocallyModified = true
	}
	s.UpdatedAt = time.Now()
	saved, storageID, err := c.repository.DeleteSkillFile(ctx, s, path)
	if err != nil {
		return nil, err
	}
	c.purge([]string{storageID})
	return &entity.DeleteSkillFileResponse{Skill: fromModelSkill(saved, on[s.ID], scope)}, nil
}

// ImportSkills copies every skill in a GitHub repository (or one directory of
// it) into the account, or only the skills req.Skills names, and turns each on
// in the workspaces req.WorkspaceIDs lists. A repository too large to import
// whole imports nothing and offers its skills to choose from instead. A skill
// whose name is already taken is reported and left alone, unless overwrite
// asks to replace it.
func (c *controller) ImportSkills(ctx context.Context, req entity.ImportSkillsRequest) (*entity.ImportSkillsResponse, error) {
	uid, _, err := c.skillScope(ctx, 0, req.UserID)
	if err != nil {
		return nil, err
	}
	if c.skillImport == nil {
		return nil, skillErr(SkillUpstream, "importing skills is not available on this server")
	}
	if len(req.Skills) > skillimport.MaxSelected {
		return nil, skillErr(SkillInvalid, "choose at most %d skills in one import", skillimport.MaxSelected)
	}
	workspaces, err := c.skillWorkspaces(ctx, req.UserID, req.WorkspaceIDs)
	if err != nil {
		return nil, err
	}
	if c.limiter != nil && !c.limiter.AllowSkillImport(uid) {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	res, err := c.skillImport.Fetch(ctx, req.URL, req.Skills)
	if err != nil {
		if errors.Is(err, skillimport.ErrInvalidURL) || errors.Is(err, skillimport.ErrNotFound) {
			return nil, skillErr(SkillInvalid, "%v", err)
		}
		return nil, skillErr(SkillUpstream, "%v", err)
	}

	own, _, err := c.repository.SearchSkills(ctx, uid, "", base.SkillFilter{}, 0, 0)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]model.Skill, len(own))
	ids := make([]int64, len(own))
	for i, s := range own {
		owned[s.Name], ids[i] = s, s.ID
	}
	on, err := c.repository.ListSkillWorkspaces(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := &entity.ImportSkillsResponse{
		Imported:     []entity.Skill{},
		Skipped:      []entity.SkillImportSkip{},
		SourceRepo:   res.Repo,
		SourceRef:    res.Ref,
		SourceCommit: res.Commit,
	}
	for _, sk := range res.Skipped {
		out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Path, Reason: sk.Reason})
	}
	for _, cd := range res.Candidates {
		out.Candidates = append(out.Candidates, entity.SkillImportCandidate{Name: cd.Name, Path: cd.Path, SkillBytes: cd.SkillBytes, Reason: cd.Reason})
	}

	for _, sk := range res.Skills {
		existing, taken := owned[sk.Name]
		if taken && !req.Overwrite {
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "your account already has a skill with this name; import again with overwrite to replace it"})
			continue
		}
		// Each skill commits on its own, so one that fails is reported with
		// the rest rather than failing an import whose earlier skills stay.
		saved, err := c.importSkill(ctx, uid, existing, taken, sk, res, workspaces)
		if err != nil {
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "could not be saved; try the import again: " + err.Error()})
			continue
		}
		out.Imported = append(out.Imported, fromModelSkill(saved, withWorkspaces(on[saved.ID], workspaces), 0))
		c.emitSkillEvent(ctx, entity.ActionSkillImport, uid, 0, saved.ID)
	}
	return out, nil
}

// skillWorkspaces checks the workspaces a skill is to be turned on in and
// returns the ones each is filed under: the parent, for a fork, and each
// once. One the caller does not own is not found.
func (c *controller) skillWorkspaces(ctx context.Context, userID string, workspaceIDs []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(workspaceIDs))
	out := make([]int64, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		_, scope, err := c.memoryOwner(ctx, id, userID)
		if err != nil {
			return nil, err
		}
		if !seen[scope] {
			seen[scope] = true
			out = append(out, scope)
		}
	}
	return out, nil
}

// withWorkspaces is the workspaces a skill on in on is on in once it is also
// turned on in more.
func withWorkspaces(on, more []int64) []int64 {
	out := slices.Clone(on)
	seen := make(map[int64]bool, len(on)+len(more))
	for _, w := range on {
		seen[w] = true
	}
	for _, w := range more {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

func (c *controller) importSkill(ctx context.Context, uid int64, existing model.Skill, taken bool, sk skillimport.Skill, res *skillimport.Result, workspaces []int64) (model.Skill, error) {
	now := time.Now()
	s := model.Skill{ID: c.idgen.NextID(), CreatedAt: now}
	if taken {
		// A re-import replaces the content, not the human's choice to turn it off.
		s.ID, s.CreatedAt, s.Disabled = existing.ID, existing.CreatedAt, existing.Disabled
	}
	s.UpdatedAt = now
	s.UserID = uid
	s.Name, s.Description = sk.Name, sk.Description
	s.SourceType, s.SourceRepo, s.SourceRef, s.SourceCommit, s.SourcePath = skillSourceGitHub, res.Repo, res.Ref, res.Commit, sk.Dir

	files := make([]model.SkillFile, 0, len(sk.Files))
	var written []string
	for _, f := range sk.Files {
		row, err := c.storeSkillFile(s, f.Path, f.Content, now)
		if err != nil {
			c.purge(written)
			return model.Skill{}, err
		}
		written = append(written, row.StorageID)
		files = append(files, row)
	}
	on := make([]model.WorkspaceSkill, len(workspaces))
	for i, w := range workspaces {
		on[i] = model.WorkspaceSkill{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, WorkspaceID: w}
	}
	saved, dropped, err := c.repository.ReplaceSkill(ctx, s, files, on)
	if err != nil {
		c.purge(written)
		return model.Skill{}, err
	}
	c.purge(dropped)
	return saved, nil
}

// purge deletes storage blobs whose rows are gone. Best effort: a blob left
// behind costs disk, while failing the call would report a delete that did
// happen as one that did not.
func (c *controller) purge(storageIDs []string) {
	for _, id := range storageIDs {
		if id != "" {
			_ = c.skillStorage.Delete(id)
		}
	}
}

// fromModelSkill is s as seen from scope, a workspace or 0 for the account,
// given the workspaces it is on in.
func fromModelSkill(s model.Skill, workspaceIDs []int64, scope int64) entity.Skill {
	return entity.Skill{
		ID:               s.ID,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		Name:             s.Name,
		Description:      s.Description,
		SourceType:       s.SourceType,
		SourceRepo:       s.SourceRepo,
		SourceRef:        s.SourceRef,
		SourceCommit:     s.SourceCommit,
		SourcePath:       s.SourcePath,
		LocallyModified:  s.LocallyModified,
		FileCount:        s.FileCount,
		TotalBytes:       s.TotalBytes,
		Enabled:          !s.Disabled,
		WorkspaceIDs:     workspaceIDs,
		WorkspaceEnabled: scope != 0 && slices.Contains(workspaceIDs, scope),
	}
}
