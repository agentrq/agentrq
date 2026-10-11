// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package skillimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

type treeItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// treeOf lists files the way GitHub's tree API does, each with the size of its
// content in files, or the size given for one not served.
func treeOf(t *testing.T, files map[string]string, extra ...treeItem) string {
	t.Helper()
	var items []treeItem
	for p, body := range files {
		items = append(items, treeItem{Path: p, Mode: "100644", Type: "blob", Size: int64(len(body))})
	}
	items = append(items, extra...)
	b, err := json.Marshal(map[string]any{"sha": testSHA, "tree": items, "truncated": false})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func rawRequests(gh *fakeGitHub) []string {
	var out []string
	for _, r := range gh.requests {
		if strings.HasPrefix(r, "/raw/") {
			out = append(out, r)
		}
	}
	return out
}

// A repository too large for the archive is listed instead, and the import
// offers its skills to choose from without reading any of them.
func TestFetch_TooLargeOffersItsSkills(t *testing.T) {
	files := map[string]string{
		"README.md":            "repo",
		"small/SKILL.md":       skillMD("small", "d"),
		"small/notes.md":       "n",
		"nested/deep/SKILL.md": skillMD("deep", "d"),
	}
	gh := &fakeGitHub{
		commitBody: testSHA,
		tarball:    []byte(strings.Repeat("x", 64)),
		treeBody: treeOf(t, files,
			treeItem{Path: "huge/SKILL.md", Mode: "100644", Type: "blob", Size: int64(skill.MaxSkillFileBytes) + 1},
			treeItem{Path: "linked/SKILL.md", Mode: "120000", Type: "blob", Size: 10},
			treeItem{Path: "nested", Mode: "040000", Type: "tree"},
			treeItem{Path: "vendor/lib", Mode: "160000", Type: "commit"},
		),
	}
	s, done := gh.server(t)
	defer done()
	s.maxDownload = 5

	res, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Repo != "a/r" || res.Ref != "main" || res.Commit != testSHA || len(res.Skills) != 0 {
		t.Fatalf("got %+v", res)
	}
	want := []Candidate{
		{Name: "huge", Path: "huge", SkillBytes: int64(skill.MaxSkillFileBytes) + 1, Reason: "SKILL.md is 98305 bytes; the limit is 96 KiB"},
		{Name: "linked", Path: "linked", SkillBytes: 10, Reason: "SKILL.md links are not imported"},
		{Name: "deep", Path: "nested/deep", SkillBytes: int64(len(files["nested/deep/SKILL.md"]))},
		{Name: "small", Path: "small", SkillBytes: int64(len(files["small/SKILL.md"]))},
	}
	slices.SortFunc(want, func(a, b Candidate) int { return strings.Compare(a.Path, b.Path) })
	if !slices.Equal(res.Candidates, want) {
		t.Errorf("candidates:\n got %+v\nwant %+v", res.Candidates, want)
	}
	if got := rawRequests(gh); len(got) != 0 {
		t.Errorf("offering must read no file, read %v", got)
	}
	if !slices.ContainsFunc(gh.requests, func(r string) bool { return r == "/api/repos/a/r/git/trees/"+testSHA }) {
		t.Errorf("the tree must be listed at the recorded commit: %v", gh.requests)
	}
}

// Chosen skills are read file by file, keeping exactly what the archive would
// have kept, and nothing of a skill not chosen is read.
func TestFetch_ChosenSkillsAreReadFileByFile(t *testing.T) {
	files := map[string]string{
		"skills/pick/SKILL.md":         skillMD("pick", "Picked.") + "Run scripts/run.sh, and see notes.md.\n",
		"skills/pick/notes.md":         "Then ref/deep.txt.",
		"skills/pick/ref/deep.txt":     "deep",
		"skills/pick/scripts/run.sh":   "#!/bin/sh\n",
		"skills/pick/unused.sh":        "nobody points here",
		"skills/pick/with space.md":    "spaced",
		"skills/pick/README.md":        "meta",
		"skills/other/SKILL.md":        skillMD("other", "Not picked."),
		"skills/other/notes.md":        "never read",
		"skills/pick/inner/SKILL.md":   skillMD("inner", "Nested, not picked."),
		"skills/pick/inner/private.md": "belongs to inner",
		".agentrq/plugin.json":         "{}",
		"docs/SKILL.md":                skillMD("docs", "Outside the link."),
	}
	gh := &fakeGitHub{
		commitBody: testSHA,
		raw:        files,
		treeBody: treeOf(t, files,
			treeItem{Path: "skills/pick/huge.txt", Mode: "100644", Type: "blob", Size: skill.MaxSubFileBytes + 1},
			treeItem{Path: "skills/pick/link.md", Mode: "120000", Type: "blob", Size: 5},
		),
	}
	s, done := gh.server(t)
	defer done()

	res, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main/skills", []string{"skills/pick", "skills/missing"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "pick" || res.Skills[0].Dir != "skills/pick" {
		t.Fatalf("skills: %+v", res.Skills)
	}
	var paths []string
	for _, f := range res.Skills[0].Files {
		paths = append(paths, f.Path)
		if f.Content == nil || string(f.Content) != files["skills/pick/"+f.Path] {
			t.Errorf("%s has content %q", f.Path, f.Content)
		}
	}
	slices.Sort(paths)
	if want := []string{"SKILL.md", "notes.md", "ref/deep.txt", "scripts/run.sh", "with space.md"}; !slices.Equal(paths, want) {
		t.Errorf("files: got %v, want %v", paths, want)
	}
	reasons := map[string]string{}
	for _, sk := range res.Skipped {
		reasons[sk.Path] = sk.Reason
	}
	for p, want := range map[string]string{
		"skills/pick/unused.sh": "is not referenced",
		"skills/pick/README.md": "is not referenced",
		"skills/pick/huge.txt":  "the limit is 64 KiB",
		"skills/pick/link.md":   "links are not imported",
		"skills/missing":        "is not a skill in this repository",
	} {
		if !strings.Contains(reasons[p], want) {
			t.Errorf("%s: reason %q, want %q", p, reasons[p], want)
		}
	}
	if len(res.Candidates) != 0 {
		t.Errorf("a chosen import offers nothing, got %+v", res.Candidates)
	}
	for _, r := range gh.requests {
		if strings.HasPrefix(r, "/codeload/") {
			t.Errorf("a chosen import must not download the archive: %s", r)
		}
		if strings.Contains(r, "/skills/other/") || strings.Contains(r, "/docs/") || strings.Contains(r, "/inner/") || strings.Contains(r, "unused") || strings.Contains(r, "README") {
			t.Errorf("read a file no chosen skill keeps: %s", r)
		}
		if strings.HasPrefix(r, "/raw/") && !strings.HasPrefix(r, "/raw/a/r/"+testSHA+"/") {
			t.Errorf("files must be read at the recorded commit: %s", r)
		}
	}
}

// A SKILL.md over the other files' limit but within its own is read whole.
func TestFetch_ChosenLargeSkillFileIsReadWhole(t *testing.T) {
	body := skillMD("big", "d") + strings.Repeat("x", skill.MaxSubFileBytes)
	files := map[string]string{"big/SKILL.md": body}
	gh := &fakeGitHub{commitStatus: http.StatusNotFound, raw: files, treeBody: treeOf(t, files)}
	s, done := gh.server(t)
	defer done()
	res, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"big"})
	if err != nil || len(res.Skills) != 1 || string(res.Skills[0].Files[0].Content) != body {
		t.Fatalf("got %+v, %v", res, err)
	}
}

// A manifest in the tree is read to say where the skills are and what they
// are called, as it is from the archive.
func TestFetch_ChosenSkillsFollowTheManifest(t *testing.T) {
	files := map[string]string{
		".agentrq/plugin.json": `{"capabilities":{"skills":[{"id":"listed","path":"tools/one"}]}}`,
		"tools/one/SKILL.md":   "---\ndescription: Named by the manifest.\n---\n",
		"tools/two/SKILL.md":   skillMD("two", "Not listed."),
	}
	gh := &fakeGitHub{commitStatus: http.StatusNotFound, raw: files, treeBody: treeOf(t, files), tarball: []byte("xxxxxxxx")}
	s, done := gh.server(t)
	defer done()
	s.maxDownload = 5

	res, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0] != (Candidate{Name: "listed", Path: "tools/one", SkillBytes: int64(len(files["tools/one/SKILL.md"]))}) {
		t.Fatalf("candidates: %+v", res.Candidates)
	}
	res, err = s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"tools/one"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "listed" {
		t.Fatalf("skills: %+v", res.Skills)
	}
	if !slices.Contains(gh.requests, "/raw/a/r/main/.agentrq/plugin.json") {
		t.Errorf("the manifest must be read at the ref when no commit is known: %v", gh.requests)
	}
}

// The archive's limits are hit before the listing is asked for, so when the
// listing fails as well, both are said.
func TestFetch_TooLargeAndUnlisted(t *testing.T) {
	gh := &fakeGitHub{commitStatus: http.StatusNotFound, tarball: []byte("xxxxxxxx"), treeStatus: http.StatusForbidden}
	s, done := gh.server(t)
	defer done()
	s.maxDownload = 5
	_, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil)
	if err == nil || !strings.Contains(err.Error(), "too large to import:") || !strings.Contains(err.Error(), "would not list its files") || !strings.Contains(err.Error(), "answered 403") {
		t.Fatalf("got %v", err)
	}
}

func TestFetch_ChosenErrors(t *testing.T) {
	ok := map[string]string{"s/SKILL.md": skillMD("s", "d")}
	for _, tc := range []struct {
		name string
		gh   fakeGitHub
		only []string
		tune func(*service)
		want string
		is   error
	}{
		{name: "no such ref", gh: fakeGitHub{treeStatus: http.StatusNotFound}, want: "no branch, tag or commit", is: ErrNotFound},
		{name: "listing refused", gh: fakeGitHub{treeStatus: http.StatusInternalServerError}, want: "answered 500 listing"},
		{name: "listing unreadable", gh: fakeGitHub{treeBody: "{"}, want: "could not be read"},
		{name: "listing truncated", gh: fakeGitHub{treeBody: `{"truncated":true,"tree":[]}`}, want: "more files than GitHub will list"},
		{name: "manifest unreadable", gh: fakeGitHub{treeBody: treeOf(t, map[string]string{".agentrq/plugin.json": "{}"}), rawStatus: http.StatusInternalServerError}, want: "answered 500 downloading .agentrq/plugin.json"},
		{name: "file unreadable", gh: fakeGitHub{treeBody: treeOf(t, ok), rawStatus: http.StatusNotFound}, want: "answered 404 downloading s/SKILL.md"},
		{name: "over the budget", gh: fakeGitHub{treeBody: treeOf(t, ok), raw: ok}, tune: func(s *service) { s.maxCollected = 4 }, want: "larger than 0 MB; choose fewer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := tc.gh
			gh.commitStatus = http.StatusNotFound
			s, done := gh.server(t)
			defer done()
			if tc.tune != nil {
				tc.tune(s)
			}
			_, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"s"})
			if err == nil || !strings.Contains(err.Error(), tc.want) || (tc.is != nil && !errors.Is(err, tc.is)) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// However small the files, one import reads a bounded number of them.
func TestFetch_ChosenFileCountIsCapped(t *testing.T) {
	files := map[string]string{"s/SKILL.md": skillMD("s", "d")}
	for i := 0; i < maxRawFiles; i++ {
		files[fmt.Sprintf("s/f%04d.md", i)] = ""
	}
	gh := &fakeGitHub{commitStatus: http.StatusNotFound, treeBody: treeOf(t, files), rawStatus: http.StatusOK}
	s, done := gh.server(t)
	defer done()
	_, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"s"})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("more than %d files", maxRawFiles)) {
		t.Fatalf("got %v", err)
	}
	if n := len(rawRequests(gh)); n != 0 {
		t.Errorf("the cap must be checked before reading, read %d", n)
	}
}

func TestFetch_ChosenNetworkErrors(t *testing.T) {
	gh := &fakeGitHub{}
	s, done := gh.server(t)
	done()
	if _, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"s"}); err == nil || !strings.Contains(err.Error(), "reach GitHub") {
		t.Errorf("listing: %v", err)
	}

	// The listing answers; the file does not arrive whole.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/git/trees/"):
			w.Write([]byte(treeOf(t, map[string]string{"s/SKILL.md": "abc"})))
		case strings.HasPrefix(r.URL.Path, "/raw/"):
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("short"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s = &service{client: srv.Client(), apiBase: srv.URL + "/api", rawBase: srv.URL + "/raw", maxCollected: maxCollectedBytes}
	if _, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"s"}); err == nil || !strings.Contains(err.Error(), "download s/SKILL.md from a/r") {
		t.Errorf("truncated file: %v", err)
	}
	s.rawBase = "http://bad host"
	if _, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", []string{"s"}); err == nil || !strings.Contains(err.Error(), "download from GitHub") {
		t.Errorf("unreachable file: %v", err)
	}
}
