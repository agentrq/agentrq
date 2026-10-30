// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// pushRoutesCtrl adds the one call mockPushController leaves out.
type pushRoutesCtrl struct {
	mockPushController
	deleteSubscriptionFunc func(ctx context.Context, req entity.DeletePushSubscriptionRequest) error
}

func (m *pushRoutesCtrl) DeleteSubscription(ctx context.Context, req entity.DeletePushSubscriptionRequest) error {
	return m.deleteSubscriptionFunc(ctx, req)
}

// pushRoutesApp mounts the push routes the way the server does, signed in as
// user, with every workspace owned by them.
func pushRoutesApp(ctrl *pushRoutesCtrl, user string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", user)
		return c.Next()
	})
	h := &handler{
		router: app.Group(""),
		crud: &mockCrudGetWorkspace{getWorkspaceFunc: func(context.Context, entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
			return &entity.GetWorkspaceResponse{}, nil
		}},
	}
	h.registerPushRoutes(ctrl)
	return app
}

func doPush(t *testing.T, app *fiber.App, method, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPushRoutes_VAPIDPublicKey(t *testing.T) {
	app := pushRoutesApp(&pushRoutesCtrl{mockPushController: mockPushController{vapidPublicKey: "BPub"}}, "u1")
	status, body := doPush(t, app, http.MethodGet, "/push/vapid-public-key", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var got struct {
		PublicKey string `json:"publicKey"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.PublicKey != "BPub" {
		t.Errorf("body = %s, want the public key", body)
	}
}

func TestPushRoutes_Unsubscribe(t *testing.T) {
	user := monoflake.ID(77).String()

	t.Run("deletes the caller's subscription", func(t *testing.T) {
		var got entity.DeletePushSubscriptionRequest
		ctrl := &pushRoutesCtrl{deleteSubscriptionFunc: func(_ context.Context, req entity.DeletePushSubscriptionRequest) error {
			got = req
			return nil
		}}
		status, _ := doPush(t, pushRoutesApp(ctrl, user), http.MethodDelete, "/push/subscribe", `{"endpoint":"https://push.example.com/s1"}`)
		if status != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", status)
		}
		if got.UserID != 77 || got.Endpoint != "https://push.example.com/s1" {
			t.Errorf("deleted %+v, want user 77's https://push.example.com/s1", got)
		}
	})

	t.Run("no endpoint is a bad payload", func(t *testing.T) {
		ctrl := &pushRoutesCtrl{deleteSubscriptionFunc: func(context.Context, entity.DeletePushSubscriptionRequest) error {
			t.Error("deleted without an endpoint")
			return nil
		}}
		if status, _ := doPush(t, pushRoutesApp(ctrl, user), http.MethodDelete, "/push/subscribe", `{}`); status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", status)
		}
	})

	t.Run("a failed delete is a 500", func(t *testing.T) {
		ctrl := &pushRoutesCtrl{deleteSubscriptionFunc: func(context.Context, entity.DeletePushSubscriptionRequest) error {
			return errors.New("db down")
		}}
		status, body := doPush(t, pushRoutesApp(ctrl, user), http.MethodDelete, "/push/subscribe", `{"endpoint":"https://push.example.com/s1"}`)
		if status != http.StatusInternalServerError || !strings.Contains(body, "failed to delete subscription") {
			t.Fatalf("status = %d body = %s, want 500 failed to delete subscription", status, body)
		}
	})
}

func TestPushRoutes_ControllerFailuresAre500(t *testing.T) {
	ws := monoflake.ID(5).String()
	ctrl := &pushRoutesCtrl{mockPushController: mockPushController{
		saveSubscriptionFunc: func(context.Context, entity.SavePushSubscriptionRequest) error {
			return errors.New("db down")
		},
		checkSubscriptionFunc: func(context.Context, entity.CheckPushSubscriptionRequest) (bool, error) {
			return false, errors.New("db down")
		},
		deleteSubscriptionByWorkspaceFunc: func(context.Context, entity.DeletePushSubscriptionByWorkspaceRequest) error {
			return errors.New("db down")
		},
	}}
	app := pushRoutesApp(ctrl, "u1")

	cases := []struct {
		method, target, body, want string
	}{
		{http.MethodPost, "/push/subscribe", `{"endpoint":"https://push.example.com/s","keys":{"p256dh":"k","auth":"a"},"workspaceId":"` + ws + `"}`, "failed to save subscription"},
		{http.MethodGet, "/push/subscription?workspaceId=" + ws + "&endpoint=https://push.example.com/s", "", "failed to check subscription"},
		{http.MethodDelete, "/push/subscription", `{"endpoint":"https://push.example.com/s","workspaceId":"` + ws + `"}`, "failed to delete subscription"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			status, body := doPush(t, app, tc.method, tc.target, tc.body)
			if status != http.StatusInternalServerError || !strings.Contains(body, tc.want) {
				t.Fatalf("status = %d body = %s, want 500 %q", status, body, tc.want)
			}
		})
	}
}
