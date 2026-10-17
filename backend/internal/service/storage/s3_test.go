// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/mustafaturan/monoflake"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	s3mocks "github.com/agentrq/agentrq/backend/internal/service/mocks/s3"
	"github.com/agentrq/agentrq/backend/internal/service/s3"
	"github.com/golang/mock/gomock"
	"gopkg.in/yaml.v3"
)

func TestS3Storage(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := s3mocks.NewMockService(ctrl)
	s := NewS3(client, "skills")
	b64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	t.Run("SaveIsPrivate", func(t *testing.T) {
		client.EXPECT().PutPrivate(gomock.Any(), "skills", "skill-1", []byte("hello"), "application/octet-stream").
			DoAndReturn(func(ctx context.Context, _, _ string, _ []byte, _ string) (string, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("S3 call has no deadline")
				}
				return "etag", nil
			})
		if err := s.Save("skill-1", b64); err != nil {
			t.Fatal(err)
		}
		client.EXPECT().PutPrivate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("S3 unavailable"))
		if err := s.Save("skill-1", b64); err == nil || err.Error() != "S3 unavailable" {
			t.Errorf("got %v", err)
		}
	})

	t.Run("Load", func(t *testing.T) {
		client.EXPECT().Get(gomock.Any(), "skills", "skill-1").Return([]byte("hello"), nil).Times(2)
		raw, err := s.LoadRaw("skill-1")
		if err != nil || string(raw) != "hello" {
			t.Fatalf("raw: %q, %v", raw, err)
		}
		enc, err := s.Load("skill-1")
		if err != nil || enc != b64 {
			t.Fatalf("load: %q, %v", enc, err)
		}
		client.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("object not found"))
		if _, err := s.Load("skill-1"); err == nil {
			t.Error("want error")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		client.EXPECT().Delete(gomock.Any(), "skills", "skill-1").Return(nil)
		if err := s.Delete("skill-1"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Refusals", func(t *testing.T) {
		// No call reaches S3 for any of these; gomock fails the test if one does.
		if err := s.Save("x", "not-base64-!!!"); err == nil {
			t.Error("bad base64 saved")
		}
		for _, id := range []string{"", ".", "..", "../x", "a/../b", "a//b", "/a", "a/"} {
			if err := s.Save(id, b64); err == nil {
				t.Errorf("save %q accepted", id)
			}
			if _, err := s.LoadRaw(id); err == nil {
				t.Errorf("load %q accepted", id)
			}
			if err := s.Delete(id); err == nil {
				t.Errorf("delete %q accepted", id)
			}
		}
	})
}

func TestS3PublicStorage(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := s3mocks.NewMockService(ctrl)
	s := NewS3Public(client, "attachments")
	b64 := base64.StdEncoding.EncodeToString([]byte("png"))
	id := monoflake.ID(3).String()
	key := AttachmentKey(1, 2, id)

	client.EXPECT().PutPrivate(gomock.Any(), "attachments", key, []byte("png"), "image/png").Return("etag", nil)
	client.EXPECT().PublicURL(gomock.Any(), "attachments", key).Return("https://cdn/attachments/" + key)
	got, err := SaveAttachment(s, 1, 2, id, b64, "image/png")
	if err != nil || got != "https://cdn/attachments/"+key {
		t.Fatalf("got %q, %v", got, err)
	}

	// A type a browser could run, or none, is stored as a download.
	for _, ct := range []string{"", "text/html", "image/svg+xml", "application/javascript"} {
		client.EXPECT().PutPrivate(gomock.Any(), "attachments", key, []byte("png"), "application/octet-stream").Return("etag", nil)
		client.EXPECT().PublicURL(gomock.Any(), "attachments", key).Return("u")
		if _, err := SaveAttachment(s, 1, 2, id, b64, ct); err != nil {
			t.Fatal(err)
		}
	}

	// A failed upload has no link.
	client.EXPECT().PutPrivate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("S3 unavailable"))
	if got, err := SaveAttachment(s, 1, 2, id, b64, "image/png"); err == nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}

	// Plain Save keeps working, as the fork and cleanup paths rely on the Service interface.
	client.EXPECT().PutPrivate(gomock.Any(), "attachments", "a4", []byte("png"), "application/octet-stream").Return("etag", nil)
	if err := s.Save("a4", b64); err != nil {
		t.Fatal(err)
	}
}

// An agent reads a file at a signed link when the bucket signs one, and at
// its public link when it does not, or cannot.
func TestS3PublicStorageReadURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := s3mocks.NewMockService(ctrl)
	s := WithFallback(NewS3Public(client, "attachments"), NewS3(client, "old"))
	key := AttachmentKey(1, 2, monoflake.ID(3).String())

	client.EXPECT().SignedURL(gomock.Any(), "attachments", key, time.Hour).Return("https://s3/signed", nil)
	if got := ReadURL(s, key, "https://s3/public", time.Hour); got != "https://s3/signed" {
		t.Fatalf("got %q, want the signed link", got)
	}

	client.EXPECT().SignedURL(gomock.Any(), "attachments", key, time.Hour).Return("", nil)
	if got := ReadURL(s, key, "https://cdn/public", time.Hour); got != "https://cdn/public" {
		t.Fatalf("got %q, want the public link when the bucket has a public base", got)
	}

	client.EXPECT().SignedURL(gomock.Any(), "attachments", key, time.Hour).Return("", errors.New("no credentials"))
	if got := ReadURL(s, key, "https://s3/public", time.Hour); got != "https://s3/public" {
		t.Fatalf("got %q, want the public link when signing fails", got)
	}

	if got := ReadURL(s, "../db", "https://s3/public", time.Hour); got != "https://s3/public" {
		t.Fatalf("got %q, want a key that is no key left unsigned", got)
	}

	// Neither a private bucket nor a local store signs.
	for _, svc := range []Service{NewS3(client, "x"), WithFallback(NewS3(client, "x"), NewS3(client, "y"))} {
		if got := ReadURL(svc, key, "https://h/public", time.Hour); got != "https://h/public" {
			t.Fatalf("got %q, want the public link as it is", got)
		}
	}
}

// fakeBucket is a path-style S3 endpoint holding objects in memory.
type fakeBucket struct {
	mu        sync.Mutex
	objects   map[string][]byte
	acls      []string
	checksums []string
	types     []string
}

func (b *fakeBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=ak/") {
		http.Error(w, "unsigned", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		b.objects[r.URL.Path] = data
		b.acls = append(b.acls, r.Header.Get("X-Amz-Acl"))
		b.types = append(b.types, r.Header.Get("Content-Type"))
		sums := r.Header.Get("X-Amz-Trailer")
		for h := range r.Header {
			if strings.HasPrefix(h, "X-Amz-Checksum-") {
				sums += h
			}
		}
		b.checksums = append(b.checksums, sums)
		w.Header().Set("ETag", `"e"`)
	case http.MethodGet:
		data, ok := b.objects[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
			return
		}
		_, _ = w.Write(data)
	case http.MethodDelete:
		delete(b.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}
}

type s3Config map[string]any

func (c s3Config) Populate(key string, out any) error {
	b, _ := yaml.Marshal(c[key])
	return yaml.Unmarshal(b, out)
}
func (s3Config) Env() string          { return "test" }
func (s3Config) App() string          { return "AgentRQ" }
func (s3Config) AppShortName() string { return "agentrq" }
func (s3Config) Version() string      { return "v0" }

// TestS3Storage_RealClient runs the adapter through the real AWS SDK against
// a local endpoint, so the key layout and the missing public ACL are what a
// bucket actually receives.
func TestS3Storage_RealClient(t *testing.T) {
	bucket := &fakeBucket{objects: map[string][]byte{}}
	srv := httptest.NewServer(bucket)
	defer srv.Close()
	client, err := s3.New(s3.Params{Config: s3Config{"s3": map[string]any{
		"endpoint": srv.URL, "accessKey": "ak", "secretAccessKey": "sk", "region": "us-east-1", "bucket": "b",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewS3(client, "skills")

	const key = "w-1/skill-2/3"
	if err := s.Save(key, base64.StdEncoding.EncodeToString([]byte("# tdd"))); err != nil {
		t.Fatal(err)
	}
	if got := string(bucket.objects["/b/skills/"+key]); got != "# tdd" {
		t.Fatalf("bucket holds %q at /b/skills/%s: %v", got, key, bucket.objects)
	}
	if len(bucket.acls) != 1 || bucket.acls[0] != "" {
		t.Errorf("a skill was uploaded with ACL %q", bucket.acls)
	}
	// Some S3-compatible stores reject the SDK's default flexible checksums.
	if len(bucket.checksums) != 1 || bucket.checksums[0] != "" {
		t.Errorf("upload asked for checksum %q", bucket.checksums)
	}
	raw, err := s.LoadRaw(key)
	if err != nil || string(raw) != "# tdd" {
		t.Fatalf("load: %q, %v", raw, err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadRaw(key); err == nil {
		t.Error("a deleted skill still loads")
	}
}

// TestS3PublicStorage_RealClient checks the link points at the object the
// real SDK uploaded, which went up with its own type and no ACL.
func TestS3PublicStorage_RealClient(t *testing.T) {
	bucket := &fakeBucket{objects: map[string][]byte{}}
	srv := httptest.NewServer(bucket)
	defer srv.Close()
	client, err := s3.New(s3.Params{Config: s3Config{"s3": map[string]any{
		"endpoint": srv.URL, "accessKey": "ak", "secretAccessKey": "sk", "region": "us-east-1", "bucket": "b",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	// Workspace 62 and task 63 are 10 and 11 in base62.
	id := monoflake.ID(64).String()
	link, err := SaveAttachment(NewS3Public(client, "attachments"), 62, 63, id, base64.StdEncoding.EncodeToString([]byte("gif")), "image/gif")
	if err != nil {
		t.Fatal(err)
	}
	key := "/b/attachments/w-00000000010/00000000011/" + id
	if link != srv.URL+key {
		t.Errorf("link %q", link)
	}
	if got := string(bucket.objects[key]); got != "gif" {
		t.Errorf("bucket holds %q: %v", got, bucket.objects)
	}
	if len(bucket.types) != 1 || bucket.types[0] != "image/gif" || bucket.acls[0] != "" {
		t.Errorf("uploaded as %q with ACL %q", bucket.types, bucket.acls)
	}
}
