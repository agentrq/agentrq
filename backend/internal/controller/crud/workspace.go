// Copyright 2026 Contextual, Inc. https://agentrq.com

package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

func (c *controller) CreateWorkspace(ctx context.Context, req entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	userID := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowWorkspace(userID) {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	now := time.Now()
	workingDirectory, err := normalizeWorkingDirectory(req.Workspace.WorkingDirectory)
	if err != nil {
		return nil, err
	}

	m := model.Workspace{
		ID:                    c.idgen.NextID(),
		CreatedAt:             now,
		UpdatedAt:             now,
		UserID:                userID,
		Name:                  req.Workspace.Name,
		Description:           req.Workspace.Description,
		AllowAllCommands:      req.Workspace.AllowAllCommands,
		SelfLearningLoopNote:  req.Workspace.SelfLearningLoopNote,
		InputSendDelaySeconds: req.Workspace.InputSendDelaySeconds,
		WorkingDirectory:      workingDirectory,
	}

	if req.Workspace.NotificationSettings != nil {
		b, _ := json.Marshal(req.Workspace.NotificationSettings)
		m.NotificationSettings = datatypes.JSON(b)
	}
	if req.Workspace.Icon != "" {
		icon, err := c.image.ResizeBase64(req.Workspace.Icon, 32, 32)
		if err == nil {
			m.Icon = icon
		}
		// If resize fails (invalid format or malicious string), we don't store it.
	}
	created, err := c.repository.CreateWorkspace(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceCreate,
		WorkspaceID:  created.ID,
		UserID:       created.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   created.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.CreateWorkspaceResponse{
		Workspace: fromModelWorkspaceToEntity(created),
	}, nil
}

func (c *controller) GetWorkspace(ctx context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}
	return &entity.GetWorkspaceResponse{Workspace: fromModelWorkspaceToEntity(m)}, nil
}

func (c *controller) CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	return c.repository.CheckWorkspaceAccess(ctx, id, uid)
}

func (c *controller) ListWorkspaces(ctx context.Context, req entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	ms, err := c.repository.ListWorkspaces(ctx, uid, req.IncludeArchived)
	if err != nil {
		return nil, err
	}
	workspaces := make([]entity.Workspace, len(ms))
	for i, m := range ms {
		workspaces[i] = fromModelWorkspaceToEntity(m)
	}
	return &entity.ListWorkspacesResponse{Workspaces: workspaces}, nil
}

func (c *controller) DeleteWorkspace(ctx context.Context, req entity.DeleteWorkspaceRequest) error {
	// 1. Get all task and message attachment IDs directly from DB
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	attachmentIDs, _ := c.repository.GetWorkspaceAttachmentIDs(ctx, req.ID)

	// 2. Delete from DB (repository handles cascaded DB delete)
	if err := c.repository.DeleteWorkspace(ctx, req.ID, uid); err != nil {
		return err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceDelete,
		WorkspaceID:  req.ID,
		UserID:       uid,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   req.ID,
		Actor:        entity.ActorHuman,
	})

	// 3. Purge storage files
	for _, id := range attachmentIDs {
		_ = c.storage.Delete(id)
	}

	return nil
}

func (c *controller) ArchiveWorkspace(ctx context.Context, req entity.ArchiveWorkspaceRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.ID, uid)
	if err != nil {
		return err
	}
	now := time.Now()
	m.ArchivedAt = &now
	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionWorkspaceUpdate,
			WorkspaceID:  updated.ID,
			UserID:       updated.UserID,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   updated.ID,
			Actor:        entity.ActorHuman,
		})
	}
	return err
}

func (c *controller) UnarchiveWorkspace(ctx context.Context, req entity.UnarchiveWorkspaceRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.ID, uid)
	if err != nil {
		return err
	}
	m.ArchivedAt = nil
	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionWorkspaceUpdate,
			WorkspaceID:  updated.ID,
			UserID:       updated.UserID,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   updated.ID,
			Actor:        entity.ActorHuman,
		})
	}
	return err
}

// normalizeWorkingDirectory validates the optional working directory a workspace
// hands to its agent, returning the value to store.
//
// The server cannot check that the directory exists: the agent usually runs on
// a different machine, so the path is only meaningful there. What it can do is
// refuse values that cannot be a directory anywhere.
//
// An absolute path is required because a relative one silently means whatever
// the agent's shell happened to start in, which is the kind of setting that
// appears to work and then does something else entirely. `~` is accepted since
// the agent expands it, and it is what a person naturally types.
func normalizeWorkingDirectory(dir string) (string, error) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		// The field is optional; blank means "wherever the agent already is".
		return "", nil
	}

	for _, r := range trimmed {
		// A newline would be a header or log injection anywhere this is echoed,
		// and no filesystem accepts a control character in a path.
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("working directory must not contain control characters")
		}
	}

	if !isAbsolutePath(trimmed) {
		return "", fmt.Errorf("working directory must be an absolute path, got %q", trimmed)
	}
	return trimmed, nil
}

// isAbsolutePath covers the shapes an absolute path takes on the platforms an
// agent might run on, rather than only the one this server happens to run on.
func isAbsolutePath(p string) bool {
	switch {
	case strings.HasPrefix(p, "/"): // POSIX
		return true
	case p == "~" || strings.HasPrefix(p, "~/"): // home, expanded by the agent
		return true
	case strings.HasPrefix(p, `\\`): // Windows UNC share
		return true
	case len(p) >= 3 && isDriveLetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/'):
		return true // Windows drive, C:\ or C:/
	default:
		return false
	}
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// validInputSendDelaySeconds are the only values the composer's send-delay
// selector may offer; anything else is rejected rather than silently clamped.
var validInputSendDelaySeconds = map[int]bool{0: true, 3: true, 5: true, 10: true, 15: true, 30: true, 60: true}

func (c *controller) UpdateWorkspace(ctx context.Context, req entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if !validInputSendDelaySeconds[req.Workspace.InputSendDelaySeconds] {
		return nil, fmt.Errorf("invalid inputSendDelaySeconds: %d", req.Workspace.InputSendDelaySeconds)
	}
	workingDirectory, err := normalizeWorkingDirectory(req.Workspace.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	m, err := c.repository.GetWorkspace(ctx, req.Workspace.ID, uid)
	if err != nil {
		return nil, err
	}
	if m.ArchivedAt != nil {
		return nil, fmt.Errorf("cannot update archived workspace")
	}

	m.Name = req.Workspace.Name
	m.Description = req.Workspace.Description
	m.AllowAllCommands = req.Workspace.AllowAllCommands
	m.SelfLearningLoopNote = req.Workspace.SelfLearningLoopNote
	m.InputSendDelaySeconds = req.Workspace.InputSendDelaySeconds
	m.WorkingDirectory = workingDirectory
	if req.Workspace.NotificationSettings != nil {
		b, _ := json.Marshal(req.Workspace.NotificationSettings)
		m.NotificationSettings = datatypes.JSON(b)
	}
	if req.Workspace.AutoAllowedTools != nil {
		b, _ := json.Marshal(req.Workspace.AutoAllowedTools)
		m.AutoAllowedTools = datatypes.JSON(b)
	}
	if req.Workspace.Icon != "" {
		icon, err := c.image.ResizeBase64(req.Workspace.Icon, 32, 32)
		if err == nil {
			m.Icon = icon
		}
		// If resize fails, we don't update/store the icon.
	}
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateWorkspace(ctx, m)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionWorkspaceUpdate,
		WorkspaceID:  updated.ID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceWorkspace,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.UpdateWorkspaceResponse{
		Workspace: fromModelWorkspaceToEntity(updated),
	}, nil
}

func (c *controller) UpdateWorkspaceAutoAllowedTools(ctx context.Context, req entity.UpdateWorkspaceAutoAllowedToolsRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetWorkspace(ctx, req.WorkspaceID, uid)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(req.Tools)
	m.AutoAllowedTools = datatypes.JSON(b)
	m.UpdatedAt = time.Now()
	_, err = c.repository.UpdateWorkspace(ctx, m)
	if err == nil {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionWorkspaceUpdate,
			WorkspaceID:  req.WorkspaceID,
			UserID:       uid,
			ResourceType: entity.ResourceWorkspace,
			ResourceID:   req.WorkspaceID,
			Actor:        entity.ActorHuman,
		})
	}
	return err
}

// statsWindow resolves a range selector into the [start, end] unix window the
// statistics queries run over.
//
// Shared by the per-workspace and account-wide endpoints deliberately: the two
// dashboards offer the same range buttons, and "this week" meaning one thing on
// one screen and something else on the other is the kind of discrepancy nobody
// reports as a bug, they just stop trusting the numbers.
//
// now is a parameter so the calendar cases can be tested at a fixed date.
func statsWindow(rng string, from, to int64, now time.Time) (startTime, endTime int64) {
	endTime = now.Unix()

	switch rng {
	case "1d":
		startTime = now.AddDate(0, 0, -1).Unix()
	case "7d":
		startTime = now.AddDate(0, 0, -7).Unix()
	case "30d":
		startTime = now.AddDate(0, 0, -30).Unix()
	case "week":
		// Full current week, Monday through Sunday, regardless of today's weekday
		daysSinceMonday := int(now.Weekday()) - 1
		if daysSinceMonday < 0 {
			daysSinceMonday = 6
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -daysSinceMonday)
		startTime = start.Unix()
		endTime = start.AddDate(0, 0, 7).Unix() - 1
	case "month":
		// Full current month, 1st through the last day, regardless of today's date
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		startTime = start.Unix()
		endTime = start.AddDate(0, 1, 0).Unix() - 1
	case "custom":
		startTime = from
		if to > 0 {
			endTime = to
		}
	default:
		// Default to 7d
		startTime = now.AddDate(0, 0, -7).Unix()
	}
	return startTime, endTime
}

func (c *controller) GetDetailedWorkspaceStats(ctx context.Context, req entity.GetWorkspaceStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	startTime, endTime := statsWindow(req.Range, req.From, req.To, time.Now())

	uid := monoflake.IDFromBase62(req.UserID).Int64()
	// Verify user ownership to prevent IDOR
	if _, err := c.repository.GetWorkspace(ctx, req.ID, uid); err != nil {
		return nil, err
	}

	res, err := c.repository.GetDetailedWorkspaceStats(ctx, req.ID, startTime, endTime)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

// GetDetailedUserStats is the account-wide counterpart: the same statistics
// summed across every workspace the caller owns.
//
// There is no ownership check because there is nothing to check against — the
// scope is the caller's own ID, taken from the session by the handler, so the
// query cannot be pointed at anyone else's data.
func (c *controller) GetDetailedUserStats(ctx context.Context, req entity.GetUserStatsRequest) (*entity.GetDetailedUserStatsResponse, error) {
	startTime, endTime := statsWindow(req.Range, req.From, req.To, time.Now())

	// No zero-check on uid: the handler rejects an unusable session before it
	// gets here, and a zero would in any case scope the query to a user that
	// owns nothing rather than widening it.
	uid := monoflake.IDFromBase62(req.UserID).Int64()

	res, err := c.repository.GetDetailedUserStats(ctx, uid, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// The repository deals in raw IDs; base62 is the API's currency.
	out := entity.GetDetailedUserStatsResponse{
		Summary:    res.Summary,
		Timeseries: res.Timeseries,
		Heatmap:    res.Heatmap,
		Workspaces: make([]entity.WorkspaceStatsBreakdown, 0, len(res.Workspaces)),
	}
	for _, row := range res.Workspaces {
		out.Workspaces = append(out.Workspaces, entity.WorkspaceStatsBreakdown{
			WorkspaceID:    monoflake.ID(row.WorkspaceID).String(),
			Name:           row.Name,
			TasksCompleted: row.TasksCompleted,
			Messages:       row.Messages,
		})
	}
	return &out, nil
}

func (c *controller) SystemGetWorkspace(ctx context.Context, id int64) (entity.Workspace, error) {
	m, err := c.repository.SystemGetWorkspace(ctx, id)
	if err != nil {
		return entity.Workspace{}, err
	}
	return fromModelWorkspaceToEntity(m), nil
}

func fromModelWorkspaceToEntity(m model.Workspace) entity.Workspace {
	res := entity.Workspace{
		ID:                    m.ID,
		CreatedAt:             m.CreatedAt,
		UpdatedAt:             m.UpdatedAt,
		UserID:                m.UserID,
		Name:                  m.Name,
		Description:           m.Description,
		Icon:                  m.Icon,
		ArchivedAt:            m.ArchivedAt,
		AutoAllowedTools:      make([]string, 0),
		AllowAllCommands:      m.AllowAllCommands,
		SelfLearningLoopNote:  m.SelfLearningLoopNote,
		InputSendDelaySeconds: m.InputSendDelaySeconds,
		WorkingDirectory:      m.WorkingDirectory,
	}
	if len(m.AutoAllowedTools) > 0 {
		_ = json.Unmarshal(m.AutoAllowedTools, &res.AutoAllowedTools)
	}
	if len(m.NotificationSettings) > 0 {
		var ns entity.NotificationSettings
		if err := json.Unmarshal(m.NotificationSettings, &ns); err == nil {
			res.NotificationSettings = &ns
		}
	}
	return res
}
