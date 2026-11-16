// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package notification

import (
	"context"
	_ "embed"
	"strings"
	"text/template"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	zlog "github.com/rs/zerolog/log"
)

const welcomeSubject = "Welcome to AgentRQ"

//go:embed welcome.txt.tmpl
var welcomeText string

var welcomeTemplate = template.Must(template.New("welcome").Parse(welcomeText))

type welcomeData struct {
	Name    string
	BaseURL string
}

// handleUserEvent sends the welcome email when an account is created, the
// one place a user is created at sign-up.
func (c *controller) handleUserEvent(ctx context.Context, event entity.CRUDEvent) {
	if event.Action != entity.ActionUserCreate || !c.smtpEnabled {
		return
	}
	u, err := c.repo.SystemGetUser(ctx, event.ResourceID)
	if err != nil {
		zlog.Error().Err(err).Int64("userID", event.ResourceID).Msg("[notification] failed to get user")
		return
	}
	if u.Email == "" {
		return
	}

	var body strings.Builder
	if err := welcomeTemplate.Execute(&body, welcomeData{Name: u.Name, BaseURL: c.baseURL}); err != nil {
		zlog.Error().Err(err).Msg("[notification] failed to render welcome email")
		return
	}

	err = c.memq.AddTask(ctx, memq.AddTaskRequest{
		QueueID: c.queueID,
		Task:    memq.Task{Val: emailTask{To: u.Email, Subject: welcomeSubject, Body: body.String()}},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("[notification] failed to enqueue welcome email")
	}
}
