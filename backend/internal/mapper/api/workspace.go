// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToCreateWorkspaceRequestEntity(c *fiber.Ctx) *entity.CreateWorkspaceRequest {
	var payload view.CreateWorkspaceRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	if payload.Workspace.Name == "" {
		return nil
	}
	return &entity.CreateWorkspaceRequest{
		Workspace: entity.Workspace{
			Name:                  payload.Workspace.Name,
			Description:           payload.Workspace.Description,
			Icon:                  payload.Workspace.Icon,
			NotificationSettings:  fromViewNotificationSettingsToEntity(payload.Workspace.NotificationSettings),
			AllowAllCommands:      payload.Workspace.AllowAllCommands,
			ClearContextDefault:   payload.Workspace.ClearContextDefault,
			SelfLearningLoopNote:  payload.Workspace.SelfLearningLoopNote,
			InputSendDelaySeconds: payload.Workspace.InputSendDelaySeconds,
			WorkingDirectory:      payload.Workspace.WorkingDirectory,
		},
	}
}

func FromCreateWorkspaceResponseEntityToHTTPResponse(rs *entity.CreateWorkspaceResponse, mcpURL string) []byte {
	payload, _ := json.Marshal(view.CreateWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(rs.Workspace, mcpURL),
	})
	return payload
}

func FromCreateWorkspaceResponseEntityToMCPResponse(rs *entity.CreateWorkspaceResponse, mcpURL string) []byte {
	w := fromEntityWorkspaceToView(rs.Workspace, mcpURL)
	w.Icon = ""
	payload, _ := json.Marshal(view.CreateWorkspaceResponse{
		Workspace: w,
	})
	return payload
}

func FromGetWorkspaceResponseEntityToHTTPResponse(rs *entity.GetWorkspaceResponse, mcpURL string) []byte {
	payload, _ := json.Marshal(view.GetWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(rs.Workspace, mcpURL),
	})
	return payload
}

func FromGetWorkspaceResponseEntityToMCPResponse(rs *entity.GetWorkspaceResponse, mcpURL string) []byte {
	w := fromEntityWorkspaceToView(rs.Workspace, mcpURL)
	w.Icon = ""
	payload, _ := json.Marshal(view.GetWorkspaceResponse{
		Workspace: w,
	})
	return payload
}

func FromListWorkspacesResponseEntityToHTTPResponse(rs *entity.ListWorkspacesResponse, mcpURLFn func(int64) string) []byte {
	workspaces := make([]view.Workspace, len(rs.Workspaces))
	for i, p := range rs.Workspaces {
		workspaces[i] = fromEntityWorkspaceToView(p, mcpURLFn(p.ID))
	}
	payload, _ := json.Marshal(view.ListWorkspacesResponse{Workspaces: workspaces})
	return payload
}

func FromListWorkspacesResponseEntityToMCPResponse(rs *entity.ListWorkspacesResponse, mcpURLFn func(int64) string) []byte {
	workspaces := make([]view.Workspace, len(rs.Workspaces))
	for i, p := range rs.Workspaces {
		w := fromEntityWorkspaceToView(p, mcpURLFn(p.ID))
		w.Icon = ""
		workspaces[i] = w
	}
	payload, _ := json.Marshal(view.ListWorkspacesResponse{Workspaces: workspaces})
	return payload
}

// FromListWorkspacesResponseEntityToMCPSummaryResponse is the supervisor's
// short list: each workspace's id, name, fork parent, folder and agent.
func FromListWorkspacesResponseEntityToMCPSummaryResponse(rs *entity.ListWorkspacesResponse) []byte {
	workspaces := make([]view.WorkspaceSummary, len(rs.Workspaces))
	for i, p := range rs.Workspaces {
		workspaces[i] = view.WorkspaceSummary{
			ID:               monoflake.ID(p.ID).String(),
			Name:             p.Name,
			WorkingDirectory: p.WorkingDirectory,
			Agent:            fromEntityAgentStateToView(p.Agent),
		}
		if p.ForkOfID != 0 {
			workspaces[i].ForkOfID = monoflake.ID(p.ForkOfID).String()
		}
	}
	payload, _ := json.Marshal(view.ListWorkspaceSummariesResponse{Workspaces: workspaces})
	return payload
}

// fromEntityAgentStateToView is what a workspace's agent is doing, nil when
// nobody asked.
func fromEntityAgentStateToView(p *entity.AgentState) *view.AgentState {
	if p == nil {
		return nil
	}
	return &view.AgentState{
		State:          p.State,
		PausedUntil:    p.PausedUntil,
		MachineID:      p.MachineID,
		Model:          p.Model,
		TaskID:         p.TaskID,
		LastActivityAt: p.LastActivityAt,
	}
}

func FromHTTPRequestToGetWorkspaceRequestEntity(c *fiber.Ctx) *entity.GetWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.GetWorkspaceRequest{ID: id}
}

func FromHTTPRequestToDeleteWorkspaceRequestEntity(c *fiber.Ctx) *entity.DeleteWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.DeleteWorkspaceRequest{ID: id}
}

func FromHTTPRequestToResetWorkspaceIDRequestEntity(c *fiber.Ctx) *entity.ResetWorkspaceIDRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.ResetWorkspaceIDRequest{ID: id}
}

func FromResetWorkspaceIDResponseEntityToHTTPResponse(rs *entity.ResetWorkspaceIDResponse) []byte {
	payload, _ := json.Marshal(view.ResetWorkspaceIDResponse{ID: monoflake.ID(rs.ID).String()})
	return payload
}

func FromHTTPRequestToUpdateWorkspaceRequestEntity(c *fiber.Ctx) *entity.UpdateWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	var payload view.UpdateWorkspaceRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	return &entity.UpdateWorkspaceRequest{
		Workspace: entity.Workspace{
			ID:                    id,
			Name:                  payload.Workspace.Name,
			Description:           payload.Workspace.Description,
			Icon:                  payload.Workspace.Icon,
			NotificationSettings:  fromViewNotificationSettingsToEntity(payload.Workspace.NotificationSettings),
			AutoAllowedTools:      payload.Workspace.AutoAllowedTools,
			AllowAllCommands:      payload.Workspace.AllowAllCommands,
			ClearContextDefault:   payload.Workspace.ClearContextDefault,
			SelfLearningLoopNote:  payload.Workspace.SelfLearningLoopNote,
			InputSendDelaySeconds: payload.Workspace.InputSendDelaySeconds,
			WorkingDirectory:      payload.Workspace.WorkingDirectory,
		},
	}
}

func FromUpdateWorkspaceResponseEntityToHTTPResponse(rs *entity.Workspace, mcpURL string) []byte {
	payload, _ := json.Marshal(view.GetWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(*rs, mcpURL),
	})
	return payload
}

func FromUpdateWorkspaceResponseEntityToMCPResponse(rs *entity.Workspace, mcpURL string) []byte {
	w := fromEntityWorkspaceToView(*rs, mcpURL)
	w.Icon = ""
	payload, _ := json.Marshal(view.GetWorkspaceResponse{
		Workspace: w,
	})
	return payload
}

func fromEntityWorkspaceToView(p entity.Workspace, mcpURL string) view.Workspace {
	v := view.Workspace{
		ID:                    monoflake.ID(p.ID).String(),
		CreatedAt:             p.CreatedAt,
		UpdatedAt:             p.UpdatedAt,
		Name:                  p.Name,
		Description:           p.Description,
		Icon:                  p.Icon,
		ArchivedAt:            p.ArchivedAt,
		NotificationSettings:  fromEntityNotificationSettingsToView(p.NotificationSettings),
		AgentConnected:        p.AgentConnected,
		Agent:                 fromEntityAgentStateToView(p.Agent),
		AgentSupportsStop:     p.AgentSupportsStop,
		AgentModels:           fromEntityAgentModelsToView(p.AgentModels),
		AgentEffort:           fromEntityAgentEffortToView(p.AgentEffort),
		AgentCommands:         fromEntityAgentCommandsToView(p.AgentCommands),
		AgentClient:           fromEntityAgentClientToView(p.AgentClient),
		AgentConcurrency:      fromEntityAgentConcurrencyToView(p.AgentConcurrency),
		MCPURL:                mcpURL,
		AutoAllowedTools:      p.AutoAllowedTools,
		AllowAllCommands:      p.AllowAllCommands,
		ClearContextDefault:   p.ClearContextDefault,
		SelfLearningLoopNote:  p.SelfLearningLoopNote,
		InputSendDelaySeconds: p.InputSendDelaySeconds,
		WorkingDirectory:      p.WorkingDirectory,
		ForkCount:             p.ForkCount,
		UnfinishedTasks:       p.UnfinishedTasks,
	}
	if p.ForkOfID != 0 {
		v.ForkOfID = monoflake.ID(p.ForkOfID).String()
	}
	if p.ForkOf != nil {
		v.ForkOf = &view.WorkspaceRef{ID: monoflake.ID(p.ForkOf.ID).String(), Name: p.ForkOf.Name}
	}
	if p.Slack != nil {
		v.Slack = &view.SlackConfig{
			Enabled:     p.Slack.Enabled,
			Installed:   p.Slack.Installed,
			ChannelID:   p.Slack.ChannelID,
			ChannelName: p.Slack.ChannelName,
			AutoCreated: p.Slack.AutoCreated,
			ClientID:    p.Slack.ClientID,
			AuthURL:     p.Slack.AuthURL,
		}
	}
	return v
}

func fromEntityNotificationSettingsToView(p *entity.NotificationSettings) *view.NotificationSettings {
	if p == nil {
		return nil
	}
	return &view.NotificationSettings{
		TaskCreated:         p.TaskCreated,
		TaskStatusUpdated:   p.TaskStatusUpdated,
		TaskReceivedMessage: p.TaskReceivedMessage,
		WorkspaceArchived:   p.WorkspaceArchived,
		WorkspaceUnarchived: p.WorkspaceUnarchived,
		Channels:            p.Channels,
	}
}

func fromViewNotificationSettingsToEntity(p *view.NotificationSettings) *entity.NotificationSettings {
	if p == nil {
		return nil
	}
	return &entity.NotificationSettings{
		TaskCreated:         p.TaskCreated,
		TaskStatusUpdated:   p.TaskStatusUpdated,
		TaskReceivedMessage: p.TaskReceivedMessage,
		WorkspaceArchived:   p.WorkspaceArchived,
		WorkspaceUnarchived: p.WorkspaceUnarchived,
		Channels:            p.Channels,
	}
}

// fromEntityAgentEffortToView renders the reasoning levels the connected agent
// offers, nil when it offers none.
func fromEntityAgentEffortToView(e *entity.AgentEffort) *view.AgentEffort {
	if e == nil {
		return nil
	}
	efforts := make([]view.AgentEffortLevel, len(e.Efforts))
	for i, one := range e.Efforts {
		efforts[i] = view.AgentEffortLevel{ID: one.ID, Name: one.Name, Description: one.Description}
	}
	return &view.AgentEffort{
		ConfigID:      e.ConfigID,
		CurrentEffort: e.CurrentEffort,
		Efforts:       efforts,
		CanSet:        e.CanSet,
	}
}

// fromEntityAgentModelsToView renders the models the connected agent offers.
//
// nil rather than an empty object when nothing is connected or nothing was
// reported: the field is omitted from the response entirely, so a client can
// read its presence as "there is a choice to make here".
func fromEntityAgentModelsToView(m *entity.AgentModels) *view.AgentModels {
	if m == nil {
		return nil
	}

	models := make([]view.AgentModel, len(m.Models))
	for i, one := range m.Models {
		models[i] = view.AgentModel{
			ID:          one.ID,
			Name:        one.Name,
			Description: one.Description,
			Current:     one.Current,
			Group:       one.Group,
		}
	}
	return &view.AgentModels{
		ConfigID:     m.ConfigID,
		CurrentModel: m.CurrentModel,
		Models:       models,
		CanSet:       m.CanSet,
	}
}

// fromEntityAgentConcurrencyToView renders how many tasks the connected gateway
// runs at once.
//
// nil rather than an empty object when nothing is connected or nothing was
// reported, for the same reason as the models above: the field is omitted from
// the response entirely, so a client can read its presence as "there is a
// number to show here".
func fromEntityAgentConcurrencyToView(c *entity.AgentConcurrency) *view.AgentConcurrency {
	if c == nil {
		return nil
	}
	return &view.AgentConcurrency{
		MaxConcurrency: c.MaxConcurrency,
		Active:         c.Active,
		Queued:         c.Queued,
		Min:            c.Min,
		Max:            c.Max,
		CanSet:         c.CanSet,
	}
}

// fromEntityAgentCommandsToView renders the slash commands the connected agent
// offers.
//
// nil rather than an empty object when nothing is connected or nothing was
// reported, for the same reason as the models above: the field is omitted from
// the response entirely, so a client can read its presence as "there is a menu
// to show here".
func fromEntityAgentCommandsToView(c *entity.AgentCommands) *view.AgentCommands {
	if c == nil {
		return nil
	}

	commands := make([]view.AgentCommand, len(c.Commands))
	for i, one := range c.Commands {
		commands[i] = view.AgentCommand{
			Name:        one.Name,
			Description: one.Description,
			Hint:        one.Hint,
		}
	}
	return &view.AgentCommands{Commands: commands}
}

// fromEntityAgentClientToView names what is attached to the workspace.
//
// nil rather than an empty object when nothing is connected, so the field's
// absence means the same thing as everywhere else on this view: there is
// nothing to show.
func fromEntityAgentClientToView(c *entity.AgentClient) *view.AgentClient {
	if c == nil {
		return nil
	}
	return &view.AgentClient{Name: c.Name, Title: c.Title, Version: c.Version}
}

// FromHTTPRequestToForkWorkspaceRequestEntity reads a fork request; the body
// is optional, and so is the name in it.
func FromHTTPRequestToForkWorkspaceRequestEntity(c *fiber.Ctx) *entity.ForkWorkspaceRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	var payload view.ForkWorkspaceRequest
	if body := c.BodyRaw(); len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil
		}
	}
	return FromMCPToForkWorkspaceRequestEntity(id, payload.Name)
}

// FromMCPToForkWorkspaceRequestEntity is a fork request from its two
// arguments, or nil when the name is too long.
func FromMCPToForkWorkspaceRequestEntity(workspaceID int64, name string) *entity.ForkWorkspaceRequest {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 128 {
		return nil
	}
	return &entity.ForkWorkspaceRequest{WorkspaceID: workspaceID, Name: name}
}

func FromForkWorkspaceResponseEntityToHTTPResponse(rs *entity.ForkWorkspaceResponse, mcpURL string) []byte {
	payload, _ := json.Marshal(view.CreateWorkspaceResponse{
		Workspace: fromEntityWorkspaceToView(rs.Workspace, mcpURL),
	})
	return payload
}

func FromForkWorkspaceResponseEntityToMCPResponse(rs *entity.ForkWorkspaceResponse, mcpURL string) []byte {
	w := fromEntityWorkspaceToView(rs.Workspace, mcpURL)
	w.Icon = ""
	payload, _ := json.Marshal(view.CreateWorkspaceResponse{
		Workspace: w,
	})
	return payload
}

func FromHTTPRequestToMergeForkRequestEntity(c *fiber.Ctx) *entity.MergeForkRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	// The body is optional: a merge with none leaves the folder alone.
	var body view.MergeForkRequest
	if len(c.Body()) > 0 {
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return nil
		}
	}
	return &entity.MergeForkRequest{WorkspaceID: id, DeleteFolder: body.DeleteFolder}
}

func FromMergeForkResponseEntityToHTTPResponse(rs *entity.MergeForkResponse) []byte {
	payload, _ := json.Marshal(view.MergeForkResponse{ParentID: rs.ParentID, MovedTasks: rs.MovedTasks})
	return payload
}
