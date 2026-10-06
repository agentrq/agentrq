// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"regexp"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/gofiber/fiber/v2"
)

// The kinds of file the public file routes serve, as they appear in a link:
// <base URL><PublicFilesPrefix>/<kind>/<key>, the layout of the storage dir.
const (
	PublicFilesPrefix    = "/storage"
	PublicFilesArtifacts = entity.PublicFileArtifacts
	PublicFilesSkills    = entity.PublicFileSkills
)

// _routePathPublicFile takes every path under the prefix, so that none of
// them falls through to the app shell; publicFilePath decides what is a link.
const _routePathPublicFile = PublicFilesPrefix + "/*"

// publicFilePath is every path a public link can have, character for
// character: fixed segments, fixed lengths, base62 and '-' only. It is matched
// against the path as it arrived, before any decoding, so '%', '.', '//' and
// the like never name a file. It captures the kind and the key.
var publicFilePath = regexp.MustCompile(`^` + PublicFilesPrefix + `/(?:` +
	`(artifacts)/(w-[0-9A-Za-z]{11}/[0-9A-Za-z]{11}/[0-9A-Za-z]{11})` +
	`|(skills)/(u-[0-9A-Za-z]{11}/skill-[0-9A-Za-z]{11}/[0-9A-Za-z]{11})` +
	`)$`)

// registerPublicFileRoutes serves attachments and skill files by their
// public link, with no session: the link is the only credential. It goes on
// the root router, outside /api/v1 and its auth, so that a
// static file server can take the same paths over.
func (h *handler) registerPublicFileRoutes(root fiber.Router) {
	root.Get(_routePathPublicFile, h.getPublicFile())
}

func (h *handler) getPublicFile() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// The first of two exact checks; the controller makes the second.
		m := publicFilePath.FindSubmatch(c.Request().URI().PathOriginal())
		if m == nil {
			return c.Status(fiber.StatusNotFound).SendString("Not Found")
		}
		kind, key := string(m[1])+string(m[3]), string(m[2])+string(m[4])
		ctx, cancel := newContext(c)
		defer cancel()
		res, err := h.crud.GetPublicFile(ctx, entity.GetPublicFileRequest{Kind: kind, Key: key})
		if err != nil {
			// The same answer for a bad link and a missing file, so a link cannot be probed.
			return c.Status(fiber.StatusNotFound).SendString("Not Found")
		}
		// Only ever a harmless type (see storage.SafeContentType), never sniffed
		// into something else, and in a sandbox that runs no script even so.
		c.Set(fiber.HeaderContentType, res.ContentType)
		c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
		c.Set(fiber.HeaderContentSecurityPolicy, "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
		if res.ContentType == "application/octet-stream" {
			c.Set(fiber.HeaderContentDisposition, "attachment")
		} else {
			c.Set(fiber.HeaderContentDisposition, "inline")
		}
		// A blob is never rewritten, only replaced under a new key.
		c.Set(fiber.HeaderCacheControl, "public, max-age=86400, immutable")
		return c.Send(res.Data)
	}
}
