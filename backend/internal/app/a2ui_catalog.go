// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import "github.com/gofiber/fiber/v3"

// a2uiCatalogHandler serves our A2UI catalog at its id's own path, so the id
// names a document every server has. The static handler before it marks a
// miss 404 and goes on, so the status is set here.
func a2uiCatalogHandler(catalog func() ([]byte, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		b, err := catalog()
		if err != nil {
			return fiber.ErrInternalServerError
		}
		c.Status(fiber.StatusOK)
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
		return c.Send(b)
	}
}
