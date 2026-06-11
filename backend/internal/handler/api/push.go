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
	r.Post("/subscribe", h.pushSubscribe(pushCtrl))
	r.Delete("/subscribe", h.pushUnsubscribe(pushCtrl))
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
		userID := monoflake.IDFromBase62(c.Locals("user_id").(string)).Int64()
		rq.UserID = userID

		ctx, cancel := newContext(c)
		defer cancel()

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
