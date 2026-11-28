// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"text/template"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	mock_memq "github.com/agentrq/agentrq/backend/internal/service/mocks/memq"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

var accountExported = entity.CRUDEvent{Action: entity.ActionAccountExport, ResourceType: entity.ResourceUser, ResourceID: 7, UserID: 7}

func fixExportClock(t *testing.T) {
	t.Helper()
	prev := exportClock
	exportClock = func() time.Time { return time.Date(2026, 11, 28, 20, 42, 0, 0, time.FixedZone("CET", 3600)) }
	t.Cleanup(func() { exportClock = prev })
}

func TestAccountExportEmail_SentToTheAccount(t *testing.T) {
	fixExportClock(t)
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	mq := mock_memq.NewMockService(ctrl)
	repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7, Email: "ada@example.com", Name: "Ada"}, nil)

	var got emailTask
	mq.EXPECT().AddTask(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req memq.AddTaskRequest) error {
		if req.QueueID != 3 {
			t.Errorf("QueueID = %d, want the email queue", req.QueueID)
		}
		got = req.Task.Val.(emailTask)
		return nil
	})

	c := &controller{repo: repo, memq: mq, queueID: 3, baseURL: "https://app.example.com", smtpEnabled: true}
	c.processEvent(context.Background(), accountExported)

	if got.To != "ada@example.com" || got.Subject != "Your AgentRQ data was downloaded" {
		t.Fatalf("email = %+v", got)
	}
	for _, want := range []string{
		"Hi Ada,",
		"requested on 28 November 2026 at 19:42 UTC",
		"If it was not, someone may be signed in to your account.",
		"https://app.example.com/settings?tab=profile",
	} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body misses %q:\n%s", want, got.Body)
		}
	}
}

func TestAccountExportEmail_GreetsAUserWithNoName(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	mq := mock_memq.NewMockService(ctrl)
	repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7, Email: "a@example.com"}, nil)
	mq.EXPECT().AddTask(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req memq.AddTaskRequest) error {
		if body := req.Task.Val.(emailTask).Body; !strings.HasPrefix(body, "Hi there,") {
			t.Errorf("body starts %q, want the nameless greeting", body[:20])
		}
		return nil
	})
	c := &controller{repo: repo, memq: mq, smtpEnabled: true}
	c.handleUserEvent(context.Background(), accountExported)
}

func TestAccountExportEmail_NotSent(t *testing.T) {
	t.Run("when SMTP is off", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		// No repository or queue call is expected.
		c := &controller{repo: mock_repository.NewMockRepository(ctrl), memq: mock_memq.NewMockService(ctrl)}
		c.handleUserEvent(context.Background(), accountExported)
	})

	t.Run("when the user has no email", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7}, nil)
		c := &controller{repo: repo, memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), accountExported)
	})

	t.Run("when the user cannot be read", func(t *testing.T) {
		logs := captureLogs(t)
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{}, errors.New("database unavailable"))
		c := &controller{repo: repo, memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), accountExported)
		logs.waitFor(t, "failed to get user")
	})

	t.Run("when the template fails", func(t *testing.T) {
		logs := captureLogs(t)
		prev := accountExportTemplate
		accountExportTemplate = template.Must(template.New("bad").Parse("{{.Missing}}"))
		t.Cleanup(func() { accountExportTemplate = prev })

		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7, Email: "a@example.com"}, nil)
		c := &controller{repo: repo, memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), accountExported)
		logs.waitFor(t, "failed to render the data export email")
	})
}

func TestAccountExportEmail_QueueFailureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	mq := mock_memq.NewMockService(ctrl)
	repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7, Email: "a@example.com"}, nil)
	mq.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(memq.ErrQueueFull)

	c := &controller{repo: repo, memq: mq, smtpEnabled: true}
	c.handleUserEvent(context.Background(), accountExported)
	logs.waitFor(t, "failed to enqueue the data export email")
}
