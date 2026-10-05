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

// SkillController manages a workspace's skills.
//
// Every write goes through here — the REST API, the MCP tools and the GitHub
// importer alike — so the rules in service/skill hold whichever door a skill
// came in by. Only the workspace that owns a skill may change it; a workspace
// it is shared into can read it and nothing else.
type SkillController interface {
	SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error)
	GetSkill(ctx context.Context, req entity.GetSkillRequest) (*entity.GetSkillResponse, error)
	SetSkillEnabled(ctx context.Context, req entity.SetSkillEnabledRequest) (*entity.SetSkillEnabledResponse, error)
	GetSkillFile(ctx context.Context, req entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error)
	SaveSkillFile(ctx context.Context, req entity.SaveSkillFileRequest) (*entity.SaveSkillFileResponse, error)
	DeleteSkill(ctx context.Context, req entity.DeleteSkillRequest) error
	DeleteSkillFile(ctx context.Context, req entity.DeleteSkillFileRequest) (*entity.DeleteSkillFileResponse, error)
	ImportSkills(ctx context.Context, req entity.ImportSkillsRequest) (*entity.ImportSkillsResponse, error)
	ListSkillShares(ctx context.Context, req entity.ListSkillSharesRequest) (*entity.ListSkillSharesResponse, error)
	ShareSkill(ctx context.Context, req entity.ShareSkillRequest) error
	UnshareSkill(ctx context.Context, req entity.ShareSkillRequest) error
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
	// SkillReadOnly is a write to a skill this workspace does not own.
	SkillReadOnly
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

// resolveSkill finds a skill this workspace can read: its own, or one shared
// into it. sharedFrom is the owning workspace for a shared one, 0 otherwise.
func (c *controller) resolveSkill(ctx context.Context, uid, workspaceID int64, name string) (model.Skill, int64, error) {
	s, err := c.repository.GetSkill(ctx, uid, workspaceID, name)
	if err == nil {
		return s, 0, nil
	}
	if !errors.Is(err, base.ErrNotFound) {
		return model.Skill{}, 0, err
	}
	shared, err := c.repository.ListSkillsSharedInto(ctx, uid, workspaceID)
	if err != nil {
		return model.Skill{}, 0, err
	}
	for _, s := range shared {
		if s.Name == name {
			return s, s.WorkspaceID, nil
		}
	}
	return model.Skill{}, 0, base.ErrNotFound
}

// ownedSkill finds a skill this workspace may change. exists is false when no
// skill of that name is here at all, which a write that creates one accepts.
func (c *controller) ownedSkill(ctx context.Context, uid, workspaceID int64, name string) (s model.Skill, exists bool, err error) {
	s, sharedFrom, err := c.resolveSkill(ctx, uid, workspaceID, name)
	if errors.Is(err, base.ErrNotFound) {
		return model.Skill{}, false, nil
	}
	if err != nil {
		return model.Skill{}, false, err
	}
	if sharedFrom != 0 {
		owner := "another workspace"
		if ws, err := c.repository.GetWorkspace(ctx, sharedFrom, uid); err == nil {
			owner = fmt.Sprintf("workspace %q", ws.Name)
		}
		return model.Skill{}, false, skillErr(SkillReadOnly, "skill %q is shared into this workspace from %s and is read-only here; change it there", name, owner)
	}
	return s, true, nil
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

// SearchSkills finds the skills a workspace can use — its own and those shared
// into it — whose name or description contains the query, ignoring case. With
// no query it lists them all, and with no limit it returns every match.
func (c *controller) SearchSkills(ctx context.Context, req entity.SearchSkillsRequest) (*entity.SearchSkillsResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
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
	limit := min(req.Limit, MaxSkillSearchLimit)
	found, total, err := c.repository.SearchSkills(ctx, uid, workspaceID, q, req.EnabledOnly, limit, req.Offset)
	if err != nil {
		return nil, err
	}
	skills := make([]entity.Skill, len(found))
	for i, s := range found {
		var sharedFrom int64
		if s.WorkspaceID != workspaceID {
			sharedFrom = s.WorkspaceID
		}
		skills[i] = fromModelSkill(s, sharedFrom)
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
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	s, sharedFrom, err := c.resolveSkill(ctx, uid, workspaceID, name)
	if err != nil {
		return nil, err
	}
	files, err := c.repository.ListSkillFiles(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	e := fromModelSkill(s, sharedFrom)
	e.Files = make([]entity.SkillFile, len(files))
	for i, f := range files {
		e.Files[i] = entity.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, URL: storage.PublicURL(c.skillStorage, f.StorageID)}
	}
	return &entity.GetSkillResponse{Skill: e}, nil
}

// SetSkillEnabled turns a skill on or off for agents. Off, it stays in the
// workspace and in the interface, but searchSkills leaves it out and loadSkill
// does not find it. The flag is the skill's own, so it holds in every
// workspace the skill is shared into, and only its owner may change it.
func (c *controller) SetSkillEnabled(ctx context.Context, req entity.SetSkillEnabledRequest) (*entity.SetSkillEnabledResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return nil, err
	}
	s, exists, err := c.ownedSkill(ctx, uid, workspaceID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, base.ErrNotFound
	}
	if s.Disabled == !req.Enabled {
		return &entity.SetSkillEnabledResponse{Skill: fromModelSkill(s, 0)}, nil
	}
	if err := c.repository.SetSkillDisabled(ctx, s.ID, !req.Enabled); err != nil {
		return nil, err
	}
	s.Disabled = !req.Enabled
	action := entity.ActionSkillEnable
	if s.Disabled {
		action = entity.ActionSkillDisable
	}
	c.emitSkillEvent(ctx, action, uid, req.WorkspaceID, s.ID)
	return &entity.SetSkillEnabledResponse{Skill: fromModelSkill(s, 0)}, nil
}

func (c *controller) GetSkillFile(ctx context.Context, req entity.GetSkillFileRequest) (*entity.GetSkillFileResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
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
	s, sharedFrom, err := c.resolveSkill(ctx, uid, workspaceID, name)
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
	c.emitSkillEvent(ctx, entity.ActionSkillView, uid, req.WorkspaceID, s.ID)
	return &entity.GetSkillFileResponse{
		Skill: fromModelSkill(s, sharedFrom),
		File:  entity.SkillFile{Path: f.Path, SizeBytes: f.SizeBytes, UpdatedAt: f.UpdatedAt, Content: string(content), URL: storage.PublicURL(c.skillStorage, f.StorageID)},
	}, nil
}

// SaveSkillFile writes one file of a skill. Saving SKILL.md creates the skill
// when there is none; any other file needs the skill to exist first, because a
// skill without a SKILL.md is invisible to every agent that might use it.
func (c *controller) SaveSkillFile(ctx context.Context, req entity.SaveSkillFileRequest) (*entity.SaveSkillFileResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
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

	s, exists, err := c.ownedSkill(ctx, uid, workspaceID, name)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if path == skill.FileName {
		fm, err := skill.ParseSkillFile(content, name)
		if err != nil {
			return nil, skillErr(SkillInvalid, "%v", err)
		}
		if fm.Name != name {
			return nil, skillErr(SkillInvalid, "%s names the skill %q, but it is being saved as %q; make the two match", skill.FileName, fm.Name, name)
		}
		if !exists {
			s = model.Skill{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, WorkspaceID: workspaceID, Name: name, SourceType: skillSourceManual}
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
	saved, replaced, err := c.repository.UpsertSkillFile(ctx, s, f)
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
		Skill: fromModelSkill(saved, 0),
		File:  entity.SkillFile{Path: path, SizeBytes: len(content), UpdatedAt: now, URL: storage.PublicURL(c.skillStorage, f.StorageID)},
	}, nil
}

// storeSkillFile writes content to storage and returns the row describing it.
// The blob goes first so a row never points at nothing; a caller whose row
// then fails to save deletes the blob.
func (c *controller) storeSkillFile(s model.Skill, path string, content []byte, now time.Time) (model.SkillFile, error) {
	sum := sha256.Sum256(content)
	f := model.SkillFile{
		ID:        c.idgen.NextID(),
		CreatedAt: now,
		UpdatedAt: now,
		Path:      path,
		SizeBytes: len(content),
		SHA256:    hex.EncodeToString(sum[:]),
		StorageID: skillStorageID(s, c.idgen.NextID()),
	}
	// Served as plain text wherever it is public, so a skill file never renders as a page.
	if _, err := storage.SaveBlob(c.skillStorage, f.StorageID, base64.StdEncoding.EncodeToString(content), skillContentType); err != nil {
		return model.SkillFile{}, fmt.Errorf("store skill file: %w", err)
	}
	return f, nil
}

const skillContentType = "text/plain; charset=utf-8"

// skillStorageID keys a skill file's blob as w-<workspace>/skill-<skill>/<blob>.
// The blob id is fresh on every write, because a replace stores the new blob
// before the old one is purged.
func skillStorageID(s model.Skill, blobID int64) string {
	return "w-" + monoflake.ID(s.WorkspaceID).String() + "/skill-" + monoflake.ID(s.ID).String() + "/" + monoflake.ID(blobID).String()
}

func (c *controller) DeleteSkill(ctx context.Context, req entity.DeleteSkillRequest) error {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return err
	}
	name, err := skillName(req.Name)
	if err != nil {
		return err
	}
	s, exists, err := c.ownedSkill(ctx, uid, workspaceID, name)
	if err != nil {
		return err
	}
	if !exists {
		return base.ErrNotFound
	}
	storageIDs, err := c.repository.DeleteSkill(ctx, s.ID)
	if err != nil {
		return err
	}
	c.purge(storageIDs)
	return nil
}

func (c *controller) DeleteSkillFile(ctx context.Context, req entity.DeleteSkillFileRequest) (*entity.DeleteSkillFileResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
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
	s, exists, err := c.ownedSkill(ctx, uid, workspaceID, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, base.ErrNotFound
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
	return &entity.DeleteSkillFileResponse{Skill: fromModelSkill(saved, 0)}, nil
}

// ImportSkills copies every skill in a GitHub repository (or one directory of
// it) into the workspace, or only the skills req.Skills names. A repository
// too large to import whole imports nothing and offers its skills to choose
// from instead. A skill whose name is already taken is reported and left
// alone, unless overwrite asks to replace one this workspace owns; a skill
// shared in from elsewhere is never replaced.
func (c *controller) ImportSkills(ctx context.Context, req entity.ImportSkillsRequest) (*entity.ImportSkillsResponse, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	if c.skillImport == nil {
		return nil, skillErr(SkillUpstream, "importing skills is not available on this server")
	}
	if c.limiter != nil && !c.limiter.AllowSkillImport(uid) {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	if len(req.Skills) > skillimport.MaxSelected {
		return nil, skillErr(SkillInvalid, "choose at most %d skills in one import", skillimport.MaxSelected)
	}
	res, err := c.skillImport.Fetch(ctx, req.URL, req.Skills)
	if err != nil {
		if errors.Is(err, skillimport.ErrInvalidURL) || errors.Is(err, skillimport.ErrNotFound) {
			return nil, skillErr(SkillInvalid, "%v", err)
		}
		return nil, skillErr(SkillUpstream, "%v", err)
	}

	own, err := c.repository.ListSkillsByWorkspace(ctx, uid, workspaceID)
	if err != nil {
		return nil, err
	}
	shared, err := c.repository.ListSkillsSharedInto(ctx, uid, workspaceID)
	if err != nil {
		return nil, err
	}
	owned := map[string]model.Skill{}
	for _, s := range own {
		owned[s.Name] = s
	}
	sharedIn := map[string]bool{}
	for _, s := range shared {
		sharedIn[s.Name] = true
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
		switch {
		case sharedIn[sk.Name]:
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "a skill shared into this workspace already has this name"})
			continue
		case taken && !req.Overwrite:
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "this workspace already has a skill with this name; import again with overwrite to replace it"})
			continue
		}
		// Each skill commits on its own, so one that fails is reported with
		// the rest rather than failing an import whose earlier skills stay.
		saved, err := c.importSkill(ctx, uid, workspaceID, existing, taken, sk, res)
		if err != nil {
			out.Skipped = append(out.Skipped, entity.SkillImportSkip{Name: sk.Name, Path: sk.Dir, Reason: "could not be saved; try the import again: " + err.Error()})
			continue
		}
		out.Imported = append(out.Imported, fromModelSkill(saved, 0))
		c.emitSkillEvent(ctx, entity.ActionSkillImport, uid, req.WorkspaceID, saved.ID)
	}
	return out, nil
}

func (c *controller) importSkill(ctx context.Context, uid, workspaceID int64, existing model.Skill, taken bool, sk skillimport.Skill, res *skillimport.Result) (model.Skill, error) {
	now := time.Now()
	s := model.Skill{ID: c.idgen.NextID(), CreatedAt: now}
	if taken {
		// A re-import replaces the content, not the human's choice to turn it off.
		s.ID, s.CreatedAt, s.Disabled = existing.ID, existing.CreatedAt, existing.Disabled
	}
	s.UpdatedAt = now
	s.UserID, s.WorkspaceID = uid, workspaceID
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
	saved, dropped, err := c.repository.ReplaceSkill(ctx, s, files)
	if err != nil {
		c.purge(written)
		return model.Skill{}, err
	}
	c.purge(dropped)
	return saved, nil
}

func (c *controller) ListSkillShares(ctx context.Context, req entity.ListSkillSharesRequest) (*entity.ListSkillSharesResponse, error) {
	s, _, err := c.sharedSkill(ctx, req.WorkspaceID, req.UserID, req.Name)
	if err != nil {
		return nil, err
	}
	shares, err := c.repository.ListSkillShares(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	out := make([]entity.SkillShare, len(shares))
	for i, sh := range shares {
		out[i] = entity.SkillShare{TargetWorkspaceID: sh.TargetWorkspaceID, CreatedAt: sh.CreatedAt}
	}
	return &entity.ListSkillSharesResponse{Shares: out}, nil
}

// ShareSkill makes a skill readable from another workspace of the same
// account. The target must not already see a different skill by that name.
func (c *controller) ShareSkill(ctx context.Context, req entity.ShareSkillRequest) error {
	s, uid, err := c.sharedSkill(ctx, req.WorkspaceID, req.UserID, req.Name)
	if err != nil {
		return err
	}
	if req.TargetWorkspaceID, err = c.shareTarget(ctx, s, req); err != nil {
		return err
	}
	if _, err := c.repository.GetSkill(ctx, uid, req.TargetWorkspaceID, s.Name); err == nil {
		return skillErr(SkillConflict, "that workspace already has its own skill called %q", s.Name)
	} else if !errors.Is(err, base.ErrNotFound) {
		return err
	}
	shared, err := c.repository.ListSkillsSharedInto(ctx, uid, req.TargetWorkspaceID)
	if err != nil {
		return err
	}
	for _, other := range shared {
		if other.Name == s.Name && other.ID != s.ID {
			return skillErr(SkillConflict, "another skill called %q is already shared into that workspace", s.Name)
		}
	}
	return c.repository.CreateSkillShare(ctx, model.SkillShare{
		ID:                c.idgen.NextID(),
		CreatedAt:         time.Now(),
		UserID:            uid,
		SkillID:           s.ID,
		TargetWorkspaceID: req.TargetWorkspaceID,
	})
}

func (c *controller) UnshareSkill(ctx context.Context, req entity.ShareSkillRequest) error {
	s, uid, err := c.sharedSkill(ctx, req.WorkspaceID, req.UserID, req.Name)
	if err != nil {
		return err
	}
	// A share into a fork was filed under its parent. A target that is gone
	// may still have a share to remove, so it is taken as it is.
	target := req.TargetWorkspaceID
	if id, err := c.ContentWorkspaceID(ctx, target, uid); err == nil {
		target = id
	}
	return c.repository.DeleteSkillShare(ctx, s.ID, target)
}

// sharedSkill is the skill a sharing call is about, which only the workspace
// that owns it may manage.
func (c *controller) sharedSkill(ctx context.Context, workspaceID int64, userID, rawName string) (model.Skill, int64, error) {
	uid, workspaceID, err := c.memoryOwner(ctx, workspaceID, userID)
	if err != nil {
		return model.Skill{}, 0, err
	}
	name, err := skillName(rawName)
	if err != nil {
		return model.Skill{}, 0, err
	}
	s, exists, err := c.ownedSkill(ctx, uid, workspaceID, name)
	if err != nil {
		return model.Skill{}, 0, err
	}
	if !exists {
		return model.Skill{}, 0, base.ErrNotFound
	}
	return s, uid, nil
}

// shareTarget checks the workspace skill s is being shared into, and returns
// the one the share is filed under: the parent, for a fork. One the caller
// does not own is not found, exactly like the source would be.
func (c *controller) shareTarget(ctx context.Context, s model.Skill, req entity.ShareSkillRequest) (int64, error) {
	ok, err := c.CheckWorkspaceAccess(ctx, req.TargetWorkspaceID, req.UserID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, base.ErrNotFound
	}
	target, err := c.ContentWorkspaceID(ctx, req.TargetWorkspaceID, s.UserID)
	if err != nil {
		return 0, err
	}
	if target == s.WorkspaceID {
		return 0, skillErr(SkillInvalid, "a skill is already available in the workspace that owns it")
	}
	return target, nil
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

func fromModelSkill(s model.Skill, sharedFrom int64) entity.Skill {
	return entity.Skill{
		ID:                    s.ID,
		CreatedAt:             s.CreatedAt,
		UpdatedAt:             s.UpdatedAt,
		WorkspaceID:           s.WorkspaceID,
		Name:                  s.Name,
		Description:           s.Description,
		SourceType:            s.SourceType,
		SourceRepo:            s.SourceRepo,
		SourceRef:             s.SourceRef,
		SourceCommit:          s.SourceCommit,
		SourcePath:            s.SourcePath,
		LocallyModified:       s.LocallyModified,
		FileCount:             s.FileCount,
		TotalBytes:            s.TotalBytes,
		Enabled:               !s.Disabled,
		SharedFromWorkspaceID: sharedFrom,
	}
}
