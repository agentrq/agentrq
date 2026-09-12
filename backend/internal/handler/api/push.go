// Copyright 2026 Contextual, Inc. https://agentrq.com

package api

import (
	"net/http"

	pushctrl "github.com/agentrq/agentrq/backend/internal/controller/push"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func (h *handler) registerPushRoutes(pushCtrl pushctrl.Controller) {
	r := h.router.Group("/push")
	r.Get("/vapid-public-key", h.getVAPIDPublicKey(pushCtrl))
	r.Get("/subscription", h.getPushSubscription(pushCtrl))
	r.Post("/subscribe", h.pushSubscribe(pushCtrl))
	r.Delete("/subscribe", h.pushUnsubscribe(pushCtrl))
	r.Delete("/subscription", h.pushUnsubscribeByWorkspace(pushCtrl))
}

func (h *handler) getVAPIDPublicKey(pushCtrl pushctrl.Controller) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromVAPIDPublicKeyToHTTPResponse(pushCtrl.VAPIDPublicKey()))
	}
}

func (h *handler) pushSubscribe(pushCtrl pushctrl.Controller) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToSavePushSubscriptionRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		userIDStr := c.Locals("user_id").(string)
		userID := monoflake.IDFromBase62(userIDStr).Int64()
		rq.UserID = userID

		ctx, cancel := newContext(c)
		defer cancel()

		// Verify the user owns the workspace before saving a subscription for it.
		if _, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: rq.WorkspaceID, UserID: userIDStr}); err != nil {
			c.Status(http.StatusForbidden)
			return c.JSON(fiber.Map{"error": "access denied"})
		}

		if err := pushCtrl.SaveSubscription(ctx, entity.SavePushSubscriptionRequest{
			UserID:      rq.UserID,
			WorkspaceID: rq.WorkspaceID,
			Endpoint:    rq.Endpoint,
			P256dh:      rq.P256dh,
			Auth:        rq.Auth,
			UserAgent:   rq.UserAgent,
			Types:       rq.Types,
		}); err != nil {
			c.Status(http.StatusInternalServerError)
			return c.JSON(fiber.Map{"error": "failed to save subscription"})
		}
		c.Status(http.StatusCreated)
		return c.JSON(fiber.Map{"status": "subscribed"})
	}
}

func (h *handler) pushUnsubscribe(pushCtrl pushctrl.Controller) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeletePushSubscriptionRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := monoflake.IDFromBase62(c.Locals("user_id").(string)).Int64()

		ctx, cancel := newContext(c)
		defer cancel()

		if err := pushCtrl.DeleteSubscription(ctx, entity.DeletePushSubscriptionRequest{
			UserID:   userID,
			Endpoint: rq.Endpoint,
		}); err != nil {
			c.Status(http.StatusInternalServerError)
			return c.JSON(fiber.Map{"error": "failed to delete subscription"})
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

func (h *handler) getPushSubscription(pushCtrl pushctrl.Controller) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		workspaceIDStr := c.Query("workspaceId")
		endpoint := c.Query("endpoint")
		if workspaceIDStr == "" || endpoint == "" {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		userIDStr := c.Locals("user_id").(string)
		userID := monoflake.IDFromBase62(userIDStr).Int64()
		workspaceID := monoflake.IDFromBase62(workspaceIDStr).Int64()

		ctx, cancel := newContext(c)
		defer cancel()

		// Verify user owns this workspace before returning subscription data.
		if _, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: workspaceID, UserID: userIDStr}); err != nil {
			c.Status(http.StatusForbidden)
			return c.JSON(fiber.Map{"error": "access denied"})
		}

		subscribed, err := pushCtrl.CheckSubscription(ctx, entity.CheckPushSubscriptionRequest{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Endpoint:    endpoint,
		})
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return c.JSON(fiber.Map{"error": "failed to check subscription"})
		}

		return c.Send(mapper.FromPushSubscriptionStatusToHTTPResponse(subscribed))
	}
}

func (h *handler) pushUnsubscribeByWorkspace(pushCtrl pushctrl.Controller) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeletePushSubscriptionByWorkspaceRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		userIDStr := c.Locals("user_id").(string)
		userID := monoflake.IDFromBase62(userIDStr).Int64()

		ctx, cancel := newContext(c)
		defer cancel()

		// Verify user owns this workspace.
		if _, err := h.crud.GetWorkspace(ctx, entity.GetWorkspaceRequest{ID: rq.WorkspaceID, UserID: userIDStr}); err != nil {
			c.Status(http.StatusForbidden)
			return c.JSON(fiber.Map{"error": "access denied"})
		}

		if err := pushCtrl.DeleteSubscriptionByWorkspace(ctx, entity.DeletePushSubscriptionByWorkspaceRequest{
			UserID:      userID,
			WorkspaceID: rq.WorkspaceID,
			Endpoint:    rq.Endpoint,
		}); err != nil {
			c.Status(http.StatusInternalServerError)
			return c.JSON(fiber.Map{"error": "failed to delete subscription"})
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}
