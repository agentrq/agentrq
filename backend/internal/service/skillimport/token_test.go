// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package skillimport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// privateGitHub serves a private repository the way GitHub does: through the
// API, with a token, and the archive from codeload on another host, the token
// for it in the redirect's URL.
type privateGitHub struct {
	mu       sync.Mutex
	tarball  []byte
	files    map[string]string
	requests []string
}

func (p *privateGitHub) servers(t *testing.T) *service {
	t.Helper()
	codeload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.log(r)
		if r.URL.Query().Get("token") != "archive" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(p.tarball)
	}))
	t.Cleanup(codeload.Close)
	// Another host name, so Go sees the redirect leave api's host.
	codeloadURL := strings.Replace(codeload.URL, "127.0.0.1", "localhost", 1)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.log(r)
		if r.Header.Get("Authorization") != "Bearer ghs_repo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch {
		case r.URL.Path == "/repos/a/r":
			w.Write([]byte(`{"default_branch":"main"}`))
		case r.URL.Path == "/repos/a/r/commits/main":
			w.Write([]byte(testSHA))
		case r.URL.Path == "/repos/a/r/tarball/"+testSHA:
			http.Redirect(w, r, codeloadURL+"/a/r/legacy.tar.gz/"+testSHA+"?token=archive", http.StatusFound)
		case r.URL.Path == "/repos/a/r/git/trees/"+testSHA:
			w.Write([]byte(treeOf(t, p.files)))
		case strings.HasPrefix(r.URL.Path, "/repos/a/r/contents/"):
			body, ok := p.files[strings.TrimPrefix(r.URL.Path, "/repos/a/r/contents/")]
			if !ok || r.URL.Query().Get("ref") != testSHA || r.Header.Get("Accept") != "application/vnd.github.raw" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)
	return &service{
		client: api.Client(), apiBase: api.URL, codeloadBase: codeloadURL, rawBase: "http://raw.invalid",
		maxDownload: maxDownloadBytes, maxExtracted: maxExtractedBytes, maxCollected: maxCollectedBytes,
	}
}

func (p *privateGitHub) log(r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r.Host+r.URL.Path+" auth="+r.Header.Get("Authorization"))
}

func TestFetch_PrivateRepositoryWithToken(t *testing.T) {
	gh := &privateGitHub{tarball: buildTarball(t, []tarEntry{{name: "a-r-sha/tdd/SKILL.md", body: skillMD("tdd", "Test first.")}})}
	s := gh.servers(t)

	res, err := s.Fetch(context.Background(), "https://github.com/a/r", nil, "ghs_repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "tdd" || res.Commit != testSHA || res.Ref != "main" {
		t.Fatalf("got %+v", res)
	}
	for _, r := range gh.requests {
		if strings.HasPrefix(r, "localhost") && !strings.HasSuffix(r, "auth=") {
			t.Errorf("the token went to codeload: %s", r)
		}
	}
	if s.token != "" {
		t.Error("Fetch kept the token on the shared importer")
	}

	// Without the token the repository is not there, and says so as GitHub
	// would for a private one.
	if _, err := s.Fetch(context.Background(), "https://github.com/a/r", nil, ""); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "is not public") {
		t.Fatalf("anonymous: %v", err)
	}
	if _, err := s.Fetch(context.Background(), "https://github.com/a/r", nil, "ghs_other"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "GitHub App was not given it") {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestFetch_PrivateRepositoryChosenFileByFile(t *testing.T) {
	gh := &privateGitHub{files: map[string]string{
		"skills/pick/SKILL.md":    skillMD("pick", "d") + "See [ref](ref/a b.md).\n",
		"skills/pick/ref/a b.md":  "spaced",
		"skills/other/SKILL.md":   skillMD("other", "d"),
		".agentrq/unrelated.json": "{}",
	}}
	s := gh.servers(t)
	res, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"skills/pick"}, "ghs_repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || len(res.Skills[0].Files) != 2 || string(res.Skills[0].Files[1].Content) != "spaced" {
		t.Fatalf("got %+v", res.Skills)
	}
}
