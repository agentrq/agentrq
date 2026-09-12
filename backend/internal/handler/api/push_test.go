// Copyright 2026 Contextual, Inc. https://agentrq.com

package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pushctrl "github.com/agentrq/agentrq/backend/internal/controller/push"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
)

// ── mock push controller ──────────────────────────────────────────────────────

type mockPushController struct {
	pushctrl.Controller
	saveSubscriptionFunc              func(ctx context.Context, req entity.SavePushSubscriptionRequest) error
	checkSubscriptionFunc             func(ctx context.Context, req entity.CheckPushSubscriptionRequest) (bool, error)
	deleteSubscriptionByWorkspaceFunc func(ctx context.Context, req entity.DeletePushSubscriptionByWorkspaceRequest) error
	vapidPublicKey                    string
}

func (m *mockPushController) SaveSubscription(ctx context.Context, req entity.SavePushSubscriptionRequest) error {
	return m.saveSubscriptionFunc(ctx, req)
}

func (m *mockPushController) CheckSubscription(ctx context.Context, req entity.CheckPushSubscriptionRequest) (bool, error) {
	return m.checkSubscriptionFunc(ctx, req)
}

func (m *mockPushController) DeleteSubscriptionByWorkspace(ctx context.Context, req entity.DeletePushSubscriptionByWorkspaceRequest) error {
	return m.deleteSubscriptionByWorkspaceFunc(ctx, req)
}

func (m *mockPushController) VAPIDPublicKey() string { return m.vapidPublicKey }
func (m *mockPushController) IsEnabled() bool        { return m.vapidPublicKey != "" }

// ── pushSubscribe ─────────────────────────────────────────────────────────────

func TestPushSubscribe_ForbiddenWhenNotOwner(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudGetWorkspace{}
	pushCtrl := &mockPushController{}

	h := &handler{crud: crudCtrl}
	app.Post("/push/subscribe", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.pushSubscribe(pushCtrl)(c)
	})

	crudCtrl.getWorkspaceFunc = func(_ context.Context, req entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		return nil, errors.New("not found")
	}

	body := []byte(`{"endpoint":"https://push.example.com/sub","keys":{"p256dh":"key","auth":"auth"},"workspaceId":"abcdef"}`)
	req := httptest.NewRequest(http.MethodPost, "/push/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestPushSubscribe_CreatesWhenOwner(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudGetWorkspace{}
	pushCtrl := &mockPushController{}

	h := &handler{crud: crudCtrl}
	app.Post("/push/subscribe", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.pushSubscribe(pushCtrl)(c)
	})

	crudCtrl.getWorkspaceFunc = func(_ context.Context, _ entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		return &entity.GetWorkspaceResponse{}, nil
	}
	pushCtrl.saveSubscriptionFunc = func(_ context.Context, _ entity.SavePushSubscriptionRequest) error {
		return nil
	}

	body := []byte(`{"endpoint":"https://push.example.com/sub","keys":{"p256dh":"key","auth":"auth"},"workspaceId":"abcdef"}`)
	req := httptest.NewRequest(http.MethodPost, "/push/subscribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestPushSubscribe_InvalidPayload(t *testing.T) {
	app := fiber.New()
	h := &handler{}
	pushCtrl := &mockPushController{}

	app.Post("/push/subscribe", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.pushSubscribe(pushCtrl)(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/push/subscribe", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

// ── getPushSubscription ───────────────────────────────────────────────────────

func TestGetPushSubscription_ForbiddenWhenNotOwner(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudGetWorkspace{}
	pushCtrl := &mockPushController{}

	h := &handler{crud: crudCtrl}
	app.Get("/push/subscription", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.getPushSubscription(pushCtrl)(c)
	})

	crudCtrl.getWorkspaceFunc = func(_ context.Context, _ entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		return nil, errors.New("not found")
	}

	req := httptest.NewRequest(http.MethodGet, "/push/subscription?workspaceId=abcdef&endpoint=https://push.example.com/sub", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestGetPushSubscription_ReturnsStatus(t *testing.T) {
	tests := []struct {
		name       string
		subscribed bool
	}{
		{"subscribed", true},
		{"not subscribed", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			crudCtrl := &mockCrudGetWorkspace{}
			pushCtrl := &mockPushController{}

			h := &handler{crud: crudCtrl}
			app.Get("/push/subscription", func(c *fiber.Ctx) error {
				c.Locals("user_id", "user1")
				return h.getPushSubscription(pushCtrl)(c)
			})

			crudCtrl.getWorkspaceFunc = func(_ context.Context, _ entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
				return &entity.GetWorkspaceResponse{}, nil
			}
			pushCtrl.checkSubscriptionFunc = func(_ context.Context, _ entity.CheckPushSubscriptionRequest) (bool, error) {
				return tt.subscribed, nil
			}

			req := httptest.NewRequest(http.MethodGet, "/push/subscription?workspaceId=abcdef&endpoint=https://push.example.com/sub", nil)
			resp, _ := app.Test(req)

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestGetPushSubscription_MissingParams(t *testing.T) {
	app := fiber.New()
	h := &handler{}
	pushCtrl := &mockPushController{}

	app.Get("/push/subscription", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.getPushSubscription(pushCtrl)(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/push/subscription?workspaceId=abcdef", nil)
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}

// ── pushUnsubscribeByWorkspace ────────────────────────────────────────────────

func TestPushUnsubscribeByWorkspace_ForbiddenWhenNotOwner(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudGetWorkspace{}
	pushCtrl := &mockPushController{}

	h := &handler{crud: crudCtrl}
	app.Delete("/push/subscription", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.pushUnsubscribeByWorkspace(pushCtrl)(c)
	})

	crudCtrl.getWorkspaceFunc = func(_ context.Context, _ entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		return nil, errors.New("not found")
	}

	body := []byte(`{"endpoint":"https://push.example.com/sub","workspaceId":"abcdef"}`)
	req := httptest.NewRequest(http.MethodDelete, "/push/subscription", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestPushUnsubscribeByWorkspace_NoContentWhenOwner(t *testing.T) {
	app := fiber.New()
	crudCtrl := &mockCrudGetWorkspace{}
	pushCtrl := &mockPushController{}

	h := &handler{crud: crudCtrl}
	app.Delete("/push/subscription", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.pushUnsubscribeByWorkspace(pushCtrl)(c)
	})

	crudCtrl.getWorkspaceFunc = func(_ context.Context, _ entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
		return &entity.GetWorkspaceResponse{}, nil
	}
	pushCtrl.deleteSubscriptionByWorkspaceFunc = func(_ context.Context, _ entity.DeletePushSubscriptionByWorkspaceRequest) error {
		return nil
	}

	body := []byte(`{"endpoint":"https://push.example.com/sub","workspaceId":"abcdef"}`)
	req := httptest.NewRequest(http.MethodDelete, "/push/subscription", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestPushUnsubscribeByWorkspace_InvalidPayload(t *testing.T) {
	app := fiber.New()
	h := &handler{}
	pushCtrl := &mockPushController{}

	app.Delete("/push/subscription", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user1")
		return h.pushUnsubscribeByWorkspace(pushCtrl)(c)
	})

	req := httptest.NewRequest(http.MethodDelete, "/push/subscription", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", resp.StatusCode)
	}
}
