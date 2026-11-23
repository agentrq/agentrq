// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package loginicon

import (
	"bytes"
	"context"
	"errors"
	stdimage "image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// pngOf is a w×h PNG.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// failingStore refuses every save; it has no public links.
type failingStore struct{ storage.Service }

func (failingStore) Save(string, string) error { return errors.New("disk full") }

// privateStore keeps blobs but gives them no public link.
type privateStore struct{ storage.Service }

func (privateStore) Save(string, string) error { return nil }

func TestUploadResizesAndKeepsTheIcon(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewNestedPublic(filepath.Join(dir, "login-icons"), "https://app.example/storage/login-icons")
	if err != nil {
		t.Fatal(err)
	}
	c := New(Params{Storage: store, Image: image.New()})

	rs, err := c.Upload(context.Background(), entity.UploadLoginIconRequest{Data: pngOf(t, 300, 200)})
	if err != nil {
		t.Fatal(err)
	}
	key, ok := strings.CutPrefix(rs.URL, "https://app.example/storage/login-icons/")
	if !ok || len(key) != 16 {
		t.Fatalf("link = %q, want one keyed by 16 hex characters", rs.URL)
	}
	raw, err := store.LoadRaw(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != Size || cfg.Height != Size {
		t.Fatalf("kept %dx%d (%v), want a %d×%d PNG", cfg.Width, cfg.Height, err, Size, Size)
	}

	again, err := c.Upload(context.Background(), entity.UploadLoginIconRequest{Data: pngOf(t, 300, 200)})
	if err != nil || again.URL != rs.URL {
		t.Errorf("the same icon again = %v, %v; want the same link", again, err)
	}
}

func TestUploadRefusals(t *testing.T) {
	ctx := context.Background()
	c := New(Params{Storage: privateStore{}, Image: image.New()})
	var refused *entity.AppConfigurationError

	if _, err := c.Upload(ctx, entity.UploadLoginIconRequest{Data: make([]byte, MaxUpload+1)}); !errors.As(err, &refused) || !strings.Contains(refused.Message, "larger than") {
		t.Errorf("an oversized icon: err = %v", err)
	}
	if _, err := c.Upload(ctx, entity.UploadLoginIconRequest{Data: []byte("<svg/>")}); !errors.As(err, &refused) || !strings.Contains(refused.Message, "PNG, JPEG, GIF or WebP") {
		t.Errorf("not an image: err = %v", err)
	}
	if _, err := c.Upload(ctx, entity.UploadLoginIconRequest{Data: pngOf(t, 4, 4)}); err == nil || !strings.Contains(err.Error(), "no public link") || errors.As(err, &refused) {
		t.Errorf("a store with no public links: err = %v", err)
	}
	c = New(Params{Storage: failingStore{}, Image: image.New()})
	if _, err := c.Upload(ctx, entity.UploadLoginIconRequest{Data: pngOf(t, 4, 4)}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("a failing store: err = %v", err)
	}
}
