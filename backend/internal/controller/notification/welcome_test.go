// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package notification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"text/template"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	mock_memq "github.com/agentrq/agentrq/backend/internal/service/mocks/memq"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

var userCreated = entity.CRUDEvent{Action: entity.ActionUserCreate, ResourceType: entity.ResourceUser, ResourceID: 7}

func TestWelcomeEmail_SentAtSignUp(t *testing.T) {
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
	c.processEvent(context.Background(), userCreated)

	if got.To != "ada@example.com" || got.Subject != "Welcome to AgentRQ" {
		t.Fatalf("email = %+v", got)
	}
	for _, want := range []string{
		"Hi Ada,",
		"https://app.example.com/",
		"https://agentrq.com/guidelines/connect-agents/claude-code-and-machines",
		"https://agentrq.com/guidelines/connect-agents/acp-gateway-and-machines",
		"https://agentrq.com/guidelines/workspaces/workspace-status-colors",
	} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body misses %q:\n%s", want, got.Body)
		}
	}
}

func TestWelcomeEmail_GreetsAUserWithNoName(t *testing.T) {
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
	c.handleUserEvent(context.Background(), userCreated)
}

func TestWelcomeEmail_NotSent(t *testing.T) {
	t.Run("when SMTP is off", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		// No repository or queue call is expected.
		c := &controller{repo: mock_repository.NewMockRepository(ctrl), memq: mock_memq.NewMockService(ctrl)}
		c.handleUserEvent(context.Background(), userCreated)
	})

	t.Run("for another user event", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		c := &controller{repo: mock_repository.NewMockRepository(ctrl), memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), entity.CRUDEvent{Action: entity.ActionUserUpdate, ResourceType: entity.ResourceUser, ResourceID: 7})
	})

	t.Run("when the user has no email", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7}, nil)
		c := &controller{repo: repo, memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), userCreated)
	})

	t.Run("when the user cannot be read", func(t *testing.T) {
		logs := captureLogs(t)
		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{}, errors.New("database unavailable"))
		c := &controller{repo: repo, memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), userCreated)
		logs.waitFor(t, "failed to get user")
	})

	t.Run("when the template fails", func(t *testing.T) {
		logs := captureLogs(t)
		prev := welcomeTemplate
		welcomeTemplate = template.Must(template.New("bad").Parse("{{.Missing}}"))
		t.Cleanup(func() { welcomeTemplate = prev })

		ctrl := gomock.NewController(t)
		repo := mock_repository.NewMockRepository(ctrl)
		repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7, Email: "a@example.com"}, nil)
		c := &controller{repo: repo, memq: mock_memq.NewMockService(ctrl), smtpEnabled: true}
		c.handleUserEvent(context.Background(), userCreated)
		logs.waitFor(t, "failed to render welcome email")
	})
}

func TestWelcomeEmail_QueueFailureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	mq := mock_memq.NewMockService(ctrl)
	repo.EXPECT().SystemGetUser(gomock.Any(), int64(7)).Return(model.User{ID: 7, Email: "a@example.com"}, nil)
	mq.EXPECT().AddTask(gomock.Any(), gomock.Any()).Return(memq.ErrQueueFull)

	c := &controller{repo: repo, memq: mq, smtpEnabled: true}
	c.handleUserEvent(context.Background(), userCreated)
	logs.waitFor(t, "failed to enqueue welcome email")
}

func TestNew_KeepsSMTPEnabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	mq := mock_memq.NewMockService(ctrl)
	mq.EXPECT().Create(gomock.Any(), gomock.Any()).Return(&memq.CreateResponse{ID: 1}, nil)
	mq.EXPECT().AddWorkers(gomock.Any(), gomock.Any()).Return(nil)
	c, err := New(Params{MemQ: mq, SMTPEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !c.(*controller).smtpEnabled {
		t.Error("SMTPEnabled was not kept")
	}
}
