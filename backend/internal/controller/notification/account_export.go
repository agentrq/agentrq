// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package notification

import (
	"context"
	_ "embed"
	"strings"
	"text/template"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	zlog "github.com/rs/zerolog/log"
)

const accountExportSubject = "Your AgentRQ data was downloaded"

//go:embed account_export.txt.tmpl
var accountExportText string

var accountExportTemplate = template.Must(template.New("accountExport").Parse(accountExportText))

type accountExportData struct {
	Name    string
	At      string
	BaseURL string
}

// exportClock is when a notice says the download was asked for; the event
// is handled as it is published.
var exportClock = time.Now

// sendAccountExportNotice tells the account its data was downloaded, so one
// taken by someone else does not go unnoticed.
func (c *controller) sendAccountExportNotice(ctx context.Context, event entity.CRUDEvent) {
	u, err := c.repo.SystemGetUser(ctx, event.ResourceID)
	if err != nil {
		zlog.Error().Err(err).Int64("userID", event.ResourceID).Msg("[notification] failed to get user")
		return
	}
	if u.Email == "" {
		return
	}

	var body strings.Builder
	at := exportClock().UTC().Format("2 January 2006 at 15:04 UTC")
	if err := accountExportTemplate.Execute(&body, accountExportData{Name: u.Name, At: at, BaseURL: c.baseURL}); err != nil {
		zlog.Error().Err(err).Msg("[notification] failed to render the data export email")
		return
	}

	err = c.memq.AddTask(ctx, memq.AddTaskRequest{
		QueueID: c.queueID,
		Task:    memq.Task{Val: emailTask{To: u.Email, Subject: accountExportSubject, Body: body.String()}},
	})
	if err != nil {
		zlog.Error().Err(err).Msg("[notification] failed to enqueue the data export email")
	}
}
