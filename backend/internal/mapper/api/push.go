package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToSavePushSubscriptionRequestEntity(c *fiber.Ctx) *entity.SavePushSubscriptionRequest {
	var payload view.PushSubscribeRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	if payload.Endpoint == "" || payload.Keys.P256dh == "" || payload.Keys.Auth == "" {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(payload.WorkspaceID).Int64()
	return &entity.SavePushSubscriptionRequest{
		Endpoint:    payload.Endpoint,
		P256dh:      payload.Keys.P256dh,
		Auth:        payload.Keys.Auth,
		WorkspaceID: workspaceID,
		UserAgent:   c.Get(fiber.HeaderUserAgent),
		Types:       payload.Types,
	}
}

func FromHTTPRequestToDeletePushSubscriptionRequestEntity(c *fiber.Ctx) *entity.DeletePushSubscriptionRequest {
	var payload view.PushUnsubscribeRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	if payload.Endpoint == "" {
		return nil
	}
	return &entity.DeletePushSubscriptionRequest{
		Endpoint: payload.Endpoint,
	}
}

func FromVAPIDPublicKeyToHTTPResponse(publicKey string) []byte {
	b, _ := json.Marshal(view.VAPIDPublicKeyResponse{PublicKey: publicKey})
	return b
}
