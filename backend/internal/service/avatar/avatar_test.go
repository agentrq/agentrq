// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package avatar

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	imagesvc "github.com/agentrq/agentrq/backend/internal/service/image"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
)

const (
	userID  = int64(42)
	urlBase = "https://cdn.example/avatars"
)

func picture(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// provider serves body at every path, with status.
func provider(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newLocal(t *testing.T) (storage.Service, string) {
	t.Helper()
	dir := t.TempDir()
	svc, err := storage.NewNestedPublic(dir, urlBase)
	if err != nil {
		t.Fatal(err)
	}
	return svc, dir
}

func TestMirror(t *testing.T) {
	ctx := context.Background()
	img := imagesvc.New()
	keyPattern := regexp.MustCompile(`^` + regexp.QuoteMeta(urlBase+"/u-"+monoflake.ID(userID).String()+"/") + `[0-9a-f]{16}\.png$`)

	t.Run("keeps a 32x32 PNG under the user and returns its public link", func(t *testing.T) {
		srv := provider(t, http.StatusOK, picture(t, 96, 96))
		store, dir := newLocal(t)
		svc := New(Params{Storage: store, Image: img, HTTPClient: srv.Client()})

		link, err := svc.Mirror(ctx, userID, srv.URL+"/a.jpg", "")
		if err != nil {
			t.Fatalf("Mirror returned %v, want a link", err)
		}
		if !keyPattern.MatchString(link) {
			t.Fatalf("the link is %q, want %s/u-<user>/<hash>.png", link, urlBase)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(link, urlBase+"/"))))
		if err != nil {
			t.Fatalf("nothing was kept at the link: %v", err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "png" || cfg.Width != Size || cfg.Height != Size {
			t.Errorf("kept a %q %dx%d (%v), want a %dx%d PNG", format, cfg.Width, cfg.Height, err, Size, Size)
		}

		again, err := svc.Mirror(ctx, userID, srv.URL+"/a.jpg", "")
		if err != nil || again != link {
			t.Errorf("the same picture again gave %q, %v, want the same link %q", again, err, link)
		}
	})

	t.Run("does not upload a picture the user already has", func(t *testing.T) {
		srv := provider(t, http.StatusOK, picture(t, 96, 96))
		store, dir := newLocal(t)
		svc := New(Params{Storage: store, Image: img, HTTPClient: srv.Client()})
		link, err := svc.Mirror(ctx, userID, srv.URL, "")
		if err != nil {
			t.Fatal(err)
		}
		kept := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(link, urlBase+"/")))
		if err := os.Remove(kept); err != nil {
			t.Fatal(err)
		}

		got, err := svc.Mirror(ctx, userID, srv.URL, link)
		if err != nil || got != link {
			t.Fatalf("Mirror returned %q, %v, want the current link %q", got, err, link)
		}
		if _, err := os.Stat(kept); !os.IsNotExist(err) {
			t.Errorf("the picture was uploaded again (%v), want it left alone", err)
		}
	})

	failures := []struct {
		name   string
		status int
		body   []byte
		src    func(srv *httptest.Server) string
		store  func(t *testing.T) storage.Service
		want   string
	}{
		{name: "a link that is not https", src: func(*httptest.Server) string { return "http://example.com/a.png" }, want: "not https"},
		{name: "a link that does not parse", src: func(*httptest.Server) string { return "https://%zz" }, want: "invalid URL escape"},
		{name: "a provider that does not answer", src: func(*httptest.Server) string { return "https://127.0.0.1:1/a.png" }, want: "connect"},
		{name: "a provider that refuses", status: http.StatusNotFound, want: "status 404"},
		{name: "a picture over the size cap", status: http.StatusOK, body: make([]byte, maxDownload+1), want: "larger than"},
		{name: "something that is not a picture", status: http.StatusOK, body: []byte("<html>"), want: "decode image"},
		{
			name: "a store that cannot save", status: http.StatusOK,
			store: func(t *testing.T) storage.Service {
				store, dir := newLocal(t)
				if err := os.WriteFile(filepath.Join(dir, "u-"+monoflake.ID(userID).String()), nil, 0o644); err != nil {
					t.Fatal(err)
				}
				return store
			},
			want: "save picture",
		},
		{
			name: "a store with no public links", status: http.StatusOK,
			store: func(t *testing.T) storage.Service {
				store, err := storage.NewNested(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				return store
			},
			want: "no public link",
		},
	}
	for _, f := range failures {
		t.Run("fails on "+f.name, func(t *testing.T) {
			body := f.body
			if body == nil && f.status == http.StatusOK {
				body = picture(t, 40, 40)
			}
			srv := provider(t, f.status, body)
			src := srv.URL + "/a.png"
			if f.src != nil {
				src = f.src(srv)
			}
			store, _ := newLocal(t)
			if f.store != nil {
				store = f.store(t)
			}
			svc := New(Params{Storage: store, Image: img, HTTPClient: srv.Client()})
			link, err := svc.Mirror(ctx, userID, src, "")
			if err == nil || !strings.Contains(err.Error(), f.want) || link != "" {
				t.Errorf("Mirror returned %q, %v, want no link and an error with %q", link, err, f.want)
			}
		})
	}

	t.Run("fails on a provider that hangs up mid-picture", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}))
		t.Cleanup(srv.Close)
		store, _ := newLocal(t)
		svc := New(Params{Storage: store, Image: img, HTTPClient: srv.Client()})
		_, err := svc.Mirror(ctx, userID, srv.URL, "")
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("Mirror returned %v, want the cut-short body's error", err)
		}
	})

	t.Run("fetches with the default client when none is given", func(t *testing.T) {
		svc := New(Params{}).(*service)
		if svc.client != http.DefaultClient {
			t.Errorf("the client is %v, want http.DefaultClient", svc.client)
		}
	})
}
