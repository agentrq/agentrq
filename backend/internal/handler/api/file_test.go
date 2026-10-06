// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/base64"
	"github.com/mustafaturan/monoflake"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/gofiber/fiber/v2"
)

// publicFileApp serves the public file routes, through the real controller,
// from a storage dir laid out as the server lays it out, with a secret beside
// the two served directories and a symlink out of one of them.
type publicFiles struct {
	app                       *fiber.App
	crud                      crud.Controller
	attKey, htmlKey, skillKey string
}

func publicFileApp(t *testing.T) publicFiles {
	t.Helper()
	dir := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "agentrq.db"), "SECRET DATABASE")
	write(filepath.Join(dir, "00000000009"), "legacy flat attachment")

	atts, err := storage.NewNestedPublic(filepath.Join(dir, "artifacts"), "u")
	if err != nil {
		t.Fatal(err)
	}
	skills, err := storage.NewNestedPublic(filepath.Join(dir, "skills"), "u")
	if err != nil {
		t.Fatal(err)
	}
	enc := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }
	f := publicFiles{
		attKey:   storage.AttachmentKey(1, 2, monoflake.ID(3).String()),
		htmlKey:  storage.AttachmentKey(1, 2, monoflake.ID(4).String()),
		skillKey: "u-00000000001/skill-00000000002/" + monoflake.ID(6).String(),
	}
	for key, content := range map[string]string{
		f.attKey:  "\x89PNG\r\n\x1a\n image",
		f.htmlKey: "<html><script>alert(1)</script></html>",
	} {
		if err := atts.Save(key, enc(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := skills.Save(f.skillKey, enc("# <b>skill</b>")); err != nil {
		t.Fatal(err)
	}
	// A link-shaped name that is a symlink to the database.
	escape := filepath.Join(dir, "artifacts", "w-00000000001", "00000000002", "00000000007")
	if err := os.Symlink(filepath.Join(dir, "agentrq.db"), escape); err != nil {
		t.Fatal(err)
	}

	f.app = fiber.New()
	f.crud = crud.New(crud.Params{Storage: storage.WithFallback(atts, mustFlat(t, dir)), SkillStorage: skills})
	h := &handler{crud: f.crud}
	h.registerPublicFileRoutes(f.app)
	return f
}

func mustFlat(t *testing.T, dir string) storage.Service {
	t.Helper()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// get sends path exactly as written, with nothing cleaned or re-encoded on the way.
func get(t *testing.T, app *fiber.App, path string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, "http://agentrq.example", nil)
	req.URL.Opaque = path
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(body)
}

func TestPublicFile_ServesALink(t *testing.T) {
	f := publicFileApp(t)

	status, h, body := get(t, f.app, "/storage/artifacts/"+f.attKey)
	if status != http.StatusOK || !strings.HasSuffix(body, " image") {
		t.Fatalf("attachment: %d %q", status, body)
	}
	for k, want := range map[string]string{
		"Content-Type":            "image/png",
		"Content-Disposition":     "inline",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	// A skill file is plain text, whatever it holds.
	status, h, body = get(t, f.app, "/storage/skills/"+f.skillKey)
	if status != http.StatusOK || body != "# <b>skill</b>" || h.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("skill: %d %q %q", status, body, h.Get("Content-Type"))
	}
}

func TestPublicFile_HTMLDownloadsRatherThanRenders(t *testing.T) {
	f := publicFileApp(t)
	status, h, _ := get(t, f.app, "/storage/artifacts/"+f.htmlKey)
	if status != http.StatusOK || h.Get("Content-Type") != "application/octet-stream" || h.Get("Content-Disposition") != "attachment" {
		t.Errorf("html served as %d %q, %q", status, h.Get("Content-Type"), h.Get("Content-Disposition"))
	}
}

func TestPublicFile_RefusesEverythingElse(t *testing.T) {
	f := publicFileApp(t)
	attKey, skillKey := f.attKey, f.skillKey
	ws, task, id := splitKey(attKey)
	for _, path := range []string{
		// Not a link at all.
		"/storage/agentrq.db",
		"/storage/00000000009",
		"/api/v1/files/attachments/" + attKey,
		"/storage/artifacts/agentrq.db",
		"/storage/artifacts/../agentrq.db",
		"/storage/artifacts/" + ws + "/../../agentrq.db",
		"/storage/artifacts/%2e%2e/agentrq.db",
		"/storage/artifacts/" + ws + "/" + task + "/..%2f..%2f..%2fagentrq.db",
		"/storage/artifacts/" + ws + "/" + task + "/..%5c..%5cagentrq.db",
		"/storage/artifacts/" + ws + "/" + task + "/" + id + "%00",
		"/storage/artifacts/" + ws + "/" + task + "/" + id + "/",
		"/storage/artifacts/" + ws + "/" + task + "//" + id,
		"/storage/artifacts//" + attKey,
		"/storage//artifacts/" + attKey,
		"/storage/artifacts/" + ws + "/" + task + "/" + id + ".png",
		"/storage/artifacts/" + ws + "/" + task + "/" + id + "x",
		"/storage/artifacts/" + ws + "/" + task + "/" + id[:len(id)-1],
		"/storage/artifacts/" + ws + "/" + task + "/" + id + "-abcdef",
		"/storage/artifacts/" + ws + "/" + task + "/" + id[:5] + "_" + id[6:],
		"/storage/artifacts/" + ws + "/" + task + "/" + strings.ToUpper(id[:1]) + "%41" + id[2:],
		"/storage/artifacts/" + ws + "/" + task,
		"/storage/artifacts/" + ws + "/" + task + "/" + id + "/" + id,
		"/storage/Attachments/" + attKey,
		"/storage/other/" + attKey,
		// A skill key under attachments, and the other way round.
		"/storage/artifacts/" + skillKey,
		// The old name is not a kind.
		"/storage/attachments/" + attKey,
		"/storage/skills/" + attKey,
		// A legacy attachment, kept flat, has no link.
		"/storage/artifacts/00000000009",
		// Link-shaped, but a symlink out of the directory.
		"/storage/artifacts/" + ws + "/" + task + "/00000000007",
		// Link-shaped, and simply not there.
		"/storage/artifacts/" + ws + "/" + task + "/00000000008",
		"/storage/artifacts/" + attKey + "?x=../../agentrq.db",
	} {
		status, _, body := get(t, f.app, path)
		if path == "/storage/artifacts/"+attKey+"?x=../../agentrq.db" {
			// A query string is not part of the path; the file is served, and nothing else.
			if status != http.StatusOK || strings.Contains(body, "SECRET") {
				t.Errorf("%s: %d %q", path, status, body)
			}
			continue
		}
		if status != http.StatusNotFound || strings.Contains(body, "SECRET") {
			t.Errorf("%s: %d %q", path, status, body)
		}
	}
}

func splitKey(key string) (ws, task, id string) {
	parts := strings.Split(key, "/")
	return parts[0], parts[1], parts[2]
}

// The file routes are registered by New ahead of the auth middleware, and
// only they are: everything under /api/v1 still wants a session.
func TestNew_PublicFilesNeedNoSession(t *testing.T) {
	f := publicFileApp(t)
	app := fiber.New()
	if _, err := New(Params{Crud: f.crud, Router: app.Group(_routeBasePath), Root: app}); err != nil {
		t.Fatal(err)
	}
	if status, _, body := get(t, app, "/storage/artifacts/"+f.attKey); status != http.StatusOK || !strings.HasSuffix(body, " image") {
		t.Errorf("public file: %d %q", status, body)
	}
	if status, _, _ := get(t, app, "/api/v1/workspaces"); status != http.StatusUnauthorized {
		t.Errorf("an API route without a session: %d", status)
	}

	// Without a root router there are no file routes at all.
	bare := fiber.New()
	if _, err := New(Params{Crud: f.crud, Router: bare.Group(_routeBasePath)}); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := get(t, bare, "/storage/artifacts/"+f.attKey); status != http.StatusNotFound {
		t.Errorf("file routes without a root: %d", status)
	}
}
