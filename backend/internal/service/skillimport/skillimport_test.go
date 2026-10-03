// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package skillimport

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

type tarEntry struct {
	name     string
	body     string
	typeflag byte
}

func buildTarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// GitHub puts a pax global header first, with no directory in its name.
	if err := tw.WriteHeader(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": testSHA}}); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0644, Typeflag: e.typeflag}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		switch hdr.Typeflag {
		case tar.TypeReg:
			hdr.Size = int64(len(e.body))
		case tar.TypeSymlink:
			hdr.Linkname = "../../etc/passwd"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func skillMD(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
}

// fakeGitHub serves the three endpoints the importer uses and records what was
// asked of it.
type fakeGitHub struct {
	repoStatus   int
	repoBody     string
	commitStatus int
	commitBody   string
	tarStatus    int
	tarball      []byte
	treeStatus   int
	treeBody     string
	// raw is the files served one by one, by their path in the repository;
	// rawStatus answers for any other.
	raw       map[string]string
	rawStatus int
	requests  []string
}

func (f *fakeGitHub) server(t *testing.T) (*service, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/repos/") && strings.Contains(r.URL.Path, "/git/trees/"):
			if r.URL.Query().Get("recursive") != "1" {
				t.Errorf("tree must be listed recursively, got %s", r.URL.RawQuery)
			}
			w.WriteHeader(orDefault(f.treeStatus))
			w.Write([]byte(f.treeBody))
		case strings.HasPrefix(r.URL.Path, "/raw/"):
			// /raw/<owner>/<repo>/<ref>/<path>
			parts := strings.SplitN(r.URL.Path, "/", 6)
			body, ok := f.raw[parts[5]]
			if !ok {
				w.WriteHeader(orDefault(f.rawStatus))
				return
			}
			w.Write([]byte(body))
		case strings.HasPrefix(r.URL.Path, "/api/repos/") && strings.Contains(r.URL.Path, "/commits/"):
			if r.Header.Get("Accept") != "application/vnd.github.sha" {
				t.Errorf("commit lookup must ask for the bare SHA, got Accept %q", r.Header.Get("Accept"))
			}
			w.WriteHeader(orDefault(f.commitStatus))
			w.Write([]byte(f.commitBody))
		case strings.HasPrefix(r.URL.Path, "/api/repos/"):
			w.WriteHeader(orDefault(f.repoStatus))
			w.Write([]byte(f.repoBody))
		case strings.HasPrefix(r.URL.Path, "/codeload/"):
			w.WriteHeader(orDefault(f.tarStatus))
			w.Write(f.tarball)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	return &service{
		client: srv.Client(), apiBase: srv.URL + "/api", codeloadBase: srv.URL + "/codeload", rawBase: srv.URL + "/raw",
		maxDownload: maxDownloadBytes, maxExtracted: maxExtractedBytes, maxCollected: maxCollectedBytes,
	}, srv.Close
}

func orDefault(status int) int {
	if status == 0 {
		return http.StatusOK
	}
	return status
}

func TestNew(t *testing.T) {
	s := New().(*service)
	if s.apiBase != "https://api.github.com" || s.codeloadBase != "https://codeload.github.com" || s.rawBase != "https://raw.githubusercontent.com" || s.client.Timeout == 0 {
		t.Errorf("New must talk to github.com with a timeout, got %+v", s)
	}
}

func TestParseURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Source
		err  bool
	}{
		{in: "https://github.com/obra/superpowers", want: Source{Owner: "obra", Repo: "superpowers"}},
		{in: " https://github.com/obra/superpowers.git/ ", want: Source{Owner: "obra", Repo: "superpowers"}},
		{in: "https://GitHub.com/obra/superpowers/tree/main", want: Source{Owner: "obra", Repo: "superpowers", Ref: "main"}},
		{in: "https://github.com/obra/superpowers/tree/v1.2/skills/tdd", want: Source{Owner: "obra", Repo: "superpowers", Ref: "v1.2", SubPath: "skills/tdd"}},
		{in: "http://github.com/obra/superpowers", err: true},
		{in: "https://evil.example/obra/superpowers", err: true},
		{in: "https://user:pw@github.com/obra/superpowers", err: true},
		{in: "https://github.com/obra", err: true},
		{in: "https://github.com/obra/superpowers/blob/main/README.md", err: true},
		{in: "https://github.com/obra/superpowers/tree", err: true},
		{in: "https://github.com/obra/super%20powers", err: true},
		{in: "https://github.com/obra/superpowers/tree/main/../../x", err: true},
		{in: "https://github.com/obra/.git", err: true},
		{in: "://bad", err: true},
	} {
		got, err := ParseURL(tc.in)
		if tc.err {
			if !errors.Is(err, ErrInvalidURL) {
				t.Errorf("ParseURL(%q): want ErrInvalidURL, got %+v, %v", tc.in, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseURL(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
}

// A repository shaped like obra/superpowers: most skills import, and the ones
// that break a rule are reported, each with the reason, instead of failing the
// whole import.
func TestFetch_SuperpowersShapedRepository(t *testing.T) {
	root := "superpowers-" + testSHA + "/"
	big := strings.Repeat("x", skill.MaxSkillFileBytes+1)
	gh := &fakeGitHub{
		repoBody:   `{"default_branch":"main"}`,
		commitBody: testSHA,
		tarball: buildTarball(t, []tarEntry{
			{name: root, typeflag: tar.TypeDir},
			{name: root + "README.md", body: "not a skill"},
			{name: root + "skills/", typeflag: tar.TypeDir},
			// Referenced from SKILL.md, then from that file, then from beside it.
			{name: root + "skills/systematic-debugging/SKILL.md", body: skillMD("systematic-debugging", "Use when debugging.") + "Start with root-cause-tracing.md.\n"},
			{name: root + "skills/systematic-debugging/root-cause-tracing.md", body: "Run ./scripts/find-polluter.sh to bisect."},
			{name: root + "skills/systematic-debugging/scripts/find-polluter.sh", body: "#!/bin/sh\n. lib.sh\n"},
			{name: root + "skills/systematic-debugging/scripts/lib.sh", body: "helpers\n"},
			{name: root + "skills/systematic-debugging/CREATION-LOG.md", body: "nobody points here"},
			{name: root + "skills/systematic-debugging/logo.png", body: "\x89PNG\x00"},
			{name: root + "skills/systematic-debugging/.DS_Store", body: "junk"},
			{name: root + "skills/systematic-debugging/link.md", typeflag: tar.TypeSymlink},
			{name: root + "skills/writing-skills/SKILL.md", body: skillMD("writing-skills", "Use when writing skills.") + "See [best practices](anthropic-best-practices.md) and huge.md.\n"},
			{name: root + "skills/writing-skills/anthropic-best-practices.md", body: strings.Repeat("y", 46*1024)},
			{name: root + "skills/writing-skills/huge.md", body: strings.Repeat("z", skill.MaxSubFileBytes+1)},
			{name: root + "skills/brainstorming/SKILL.md", body: "---\nname: brainstorming\ndescription: d\n---\n" + big},
			{name: root + "skills/no-description/SKILL.md", body: "---\nname: no-description\n---\n"},
			{name: root + "skills/linked/SKILL.md", typeflag: tar.TypeSymlink},
			{name: root + "skills/dupe-a/SKILL.md", body: skillMD("same-name", "First.")},
			{name: root + "skills/dupe-b/SKILL.md", body: skillMD("same-name", "Second.")},
			{name: root + "skills/outer/SKILL.md", body: "---\ndescription: Outer skill, named by its directory.\n---\nRead `notes.md`, then inner/notes.md.\n"},
			{name: root + "skills/outer/notes.md", body: "outer notes"},
			// Contains "notes.md", but is not a mention of it.
			{name: root + "skills/outer/xnotes.md", body: "not referenced"},
			{name: root + "skills/outer/inner/SKILL.md", body: skillMD("inner", "Inner skill.") + "Anything in `refs/` helps.\n"},
			{name: root + "skills/outer/inner/refs/a.md", body: "a"},
			{name: root + "skills/outer/inner/refs/b.md", body: "b"},
		}),
	}
	s, done := gh.server(t)
	defer done()

	res, err := s.Fetch(context.Background(), "https://github.com/obra/superpowers", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Repo != "obra/superpowers" || res.Ref != "main" || res.Commit != testSHA {
		t.Errorf("source: %+v", res)
	}
	// The archive is fetched by the commit that was recorded, not by the ref.
	if last := gh.requests[len(gh.requests)-1]; last != "/codeload/obra/superpowers/tar.gz/"+testSHA {
		t.Errorf("archive request: %s", last)
	}

	got := map[string]Skill{}
	for _, sk := range res.Skills {
		got[sk.Name] = sk
	}
	wantFiles := map[string][]string{
		"inner":                {"SKILL.md", "refs/a.md", "refs/b.md"},
		"outer":                {"SKILL.md", "notes.md", "xnotes.md"},
		"same-name":            {"SKILL.md"},
		"systematic-debugging": {"SKILL.md", "root-cause-tracing.md", "scripts/find-polluter.sh", "scripts/lib.sh", "CREATION-LOG.md"},
		"writing-skills":       {"SKILL.md", "anthropic-best-practices.md"},
	}
	if len(got) != len(wantFiles) {
		t.Fatalf("imported %d skills, want %d: %+v", len(got), len(wantFiles), res.Skills)
	}
	for name, files := range wantFiles {
		sk, ok := got[name]
		if !ok {
			t.Errorf("%s was not imported", name)
			continue
		}
		var paths []string
		for _, f := range sk.Files {
			paths = append(paths, f.Path)
		}
		if strings.Join(paths, ",") != strings.Join(files, ",") {
			t.Errorf("%s files: %v, want %v", name, paths, files)
		}
	}
	if got["same-name"].Description != "First." || got["same-name"].Dir != "skills/dupe-a" {
		t.Errorf("the first of two same-named skills wins: %+v", got["same-name"])
	}

	reasons := map[string]string{}
	for _, sk := range res.Skipped {
		reasons[sk.Path] = sk.Reason
	}
	for p, want := range map[string]string{
		"skills/brainstorming":                  "96 KiB",
		"skills/no-description":                 "needs a description",
		"skills/linked":                         "links are not imported",
		"skills/dupe-b":                         "already called",
		"skills/systematic-debugging/logo.png":  "not UTF-8 text",
		"skills/systematic-debugging/.DS_Store": "hidden",
		"skills/systematic-debugging/link.md":   "links are not imported",
		"skills/writing-skills/huge.md":         "64 KiB",
	} {
		if !strings.Contains(reasons[p], want) {
			t.Errorf("skipped %s: reason %q, want it to mention %q", p, reasons[p], want)
		}
	}
	if len(res.Skipped) != 8 {
		t.Errorf("skipped %d entries, want 8: %+v", len(res.Skipped), res.Skipped)
	}
}

// A link to one directory imports only what is under it, and a skill at the
// root of that directory is named after it.
func TestFetch_SubPathAndRootSkill(t *testing.T) {
	root := "repo-main/"
	gh := &fakeGitHub{
		commitStatus: http.StatusForbidden, // rate limited: the SHA is best effort
		tarball: buildTarball(t, []tarEntry{
			{name: root + "SKILL.md", body: skillMD("elsewhere", "Not under the link.")},
			{name: root + "skills/pr-reviewer/SKILL.md", body: "---\ndescription: Reviews PRs.\n---\nWork through checklist.md\n"},
			{name: root + "skills/pr-reviewer/checklist.md", body: "- tests"},
		}),
	}
	s, done := gh.server(t)
	defer done()

	res, err := s.Fetch(context.Background(), "https://github.com/acme/repo/tree/main/skills/pr-reviewer", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Commit != "" || res.Ref != "main" {
		t.Errorf("source: %+v", res)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "pr-reviewer" || res.Skills[0].Dir != "skills/pr-reviewer" || len(res.Skills[0].Files) != 2 {
		t.Fatalf("skills: %+v", res.Skills)
	}
	// No default-branch lookup when the link names the ref.
	for _, r := range gh.requests {
		if r == "/api/repos/acme/repo" {
			t.Error("looked up the default branch although the link named one")
		}
	}
	if last := gh.requests[len(gh.requests)-1]; last != "/codeload/acme/repo/tar.gz/main" {
		t.Errorf("without a SHA the ref is downloaded: %s", last)
	}
}

func TestFetch_RepositoryRootSkillIsNamedAfterTheRepository(t *testing.T) {
	gh := &fakeGitHub{
		repoBody:   `{"default_branch":"trunk"}`,
		commitBody: "not a sha",
		tarball:    buildTarball(t, []tarEntry{{name: "my-skill-trunk/SKILL.md", body: "---\ndescription: Root skill.\n---\n"}}),
	}
	s, done := gh.server(t)
	defer done()

	res, err := s.Fetch(context.Background(), "https://github.com/acme/my-skill", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "my-skill" || res.Skills[0].Dir != "" || res.Commit != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestFetch_Limits(t *testing.T) {
	root := "repo-main/"
	var many []tarEntry
	many = append(many, tarEntry{name: root + "wide/SKILL.md", body: skillMD("wide", "Too many files.") + "Everything in `files/`.\n"})
	for i := 0; i < skill.MaxFiles; i++ {
		many = append(many, tarEntry{name: root + fmt.Sprintf("wide/files/f%03d.md", i), body: "x"})
	}
	// Enough 60 KB files to pass the 2 MB import budget across two skills.
	chunk := strings.Repeat("c", 60*1024)
	for _, dir := range []string{"a-first", "b-second"} {
		many = append(many, tarEntry{name: root + dir + "/SKILL.md", body: skillMD(dir, "Big.") + "See parts/.\n"})
		for i := 0; i < 20; i++ {
			many = append(many, tarEntry{name: root + dir + "/parts/" + string(rune('a'+i)) + ".md", body: chunk})
		}
	}
	gh := &fakeGitHub{commitBody: testSHA, tarball: buildTarball(t, many)}
	s, done := gh.server(t)
	defer done()

	res, err := s.Fetch(context.Background(), "https://github.com/acme/repo/tree/main", nil)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "a-first" {
		t.Fatalf("only the first big skill fits: %+v", names(res.Skills))
	}
	reasons := map[string]string{}
	for _, sk := range res.Skipped {
		reasons[sk.Path] = sk.Reason
	}
	if !strings.Contains(reasons["wide"], fmt.Sprintf("files; the limit is %d", skill.MaxFiles)) {
		t.Errorf("wide: %q", reasons["wide"])
	}
	if !strings.Contains(reasons["b-second"], "2 MB limit") {
		t.Errorf("b-second: %q", reasons["b-second"])
	}
}

func names(skills []Skill) []string {
	var out []string
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

func TestFetch_Errors(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		gh   fakeGitHub
		want string
		is   error
	}{
		{name: "bad url", url: "https://gitlab.com/a/b", is: ErrInvalidURL},
		{name: "repo missing", url: "https://github.com/a/b", gh: fakeGitHub{repoStatus: 404}, is: ErrNotFound},
		{name: "repo lookup rate limited", url: "https://github.com/a/b", gh: fakeGitHub{repoStatus: 403}, want: "name a branch"},
		{name: "repo body garbage", url: "https://github.com/a/b", gh: fakeGitHub{repoBody: "{"}, want: "did not say which branch"},
		{name: "default branch unusable", url: "https://github.com/a/b", gh: fakeGitHub{repoBody: `{"default_branch":"a b"}`}, want: "did not say which branch"},
		{name: "ref missing", url: "https://github.com/a/b/tree/nope", gh: fakeGitHub{commitStatus: 404, tarStatus: 404}, is: ErrNotFound},
		{name: "download fails", url: "https://github.com/a/b/tree/main", gh: fakeGitHub{commitStatus: 404, tarStatus: 500}, want: "answered 500"},
		{name: "not gzip", url: "https://github.com/a/b/tree/main", gh: fakeGitHub{commitStatus: 404, tarball: []byte("plain")}, want: "could not be read"},
		{name: "corrupt tar", url: "https://github.com/a/b/tree/main", gh: fakeGitHub{commitStatus: 404, tarball: gzipped([]byte(strings.Repeat("\x01", 1024)))}, want: "could not be read"},
		{name: "empty archive", url: "https://github.com/a/b/tree/main", gh: fakeGitHub{commitStatus: 404, tarball: gzipped(nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := tc.gh
			s, done := gh.server(t)
			defer done()
			res, err := s.Fetch(context.Background(), tc.url, nil)
			if tc.want == "" && tc.is == nil {
				// An empty archive is not an error, just an import of nothing.
				if err != nil || len(res.Skills) != 0 {
					t.Fatalf("got %+v, %v", res, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error, got %+v", res)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("want %v, got %v", tc.is, err)
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want %q in %v", tc.want, err)
			}
		})
	}
}

func gzipped(b []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(b)
	gz.Close()
	return buf.Bytes()
}

// A truncated file inside an otherwise valid archive is a read error, not an
// import of half a file.
func TestFetch_TruncatedFileBody(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	tw.WriteHeader(&tar.Header{Name: "r-main/SKILL.md", Typeflag: tar.TypeReg, Size: 100, Mode: 0644})
	tw.Write([]byte("short"))
	gh := &fakeGitHub{commitStatus: 404, tarball: gzipped(raw.Bytes())}
	s, done := gh.server(t)
	defer done()
	if _, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("got %v", err)
	}
}

func TestFetch_NetworkError(t *testing.T) {
	gh := &fakeGitHub{}
	s, done := gh.server(t)
	done() // nothing is listening any more
	if _, err := s.Fetch(context.Background(), "https://github.com/a/b", nil); err == nil || !strings.Contains(err.Error(), "reach GitHub") {
		t.Errorf("repo lookup: %v", err)
	}
	if _, err := s.Fetch(context.Background(), "https://github.com/a/b/tree/main", nil); err == nil || !strings.Contains(err.Error(), "download from GitHub") {
		t.Errorf("download: %v", err)
	}
}

func TestGet_BadURL(t *testing.T) {
	s := &service{client: http.DefaultClient}
	if _, err := s.get(context.Background(), "http://bad host/", ""); err == nil {
		t.Error("want an error for an unparseable URL")
	}
}

// Each budget stops the download with the same advice: link to a directory.
func TestFetch_TooLarge(t *testing.T) {
	var entries []tarEntry
	for i := 0; i < 8; i++ {
		entries = append(entries, tarEntry{name: "r-main/f" + string(rune('a'+i)) + ".md", body: strings.Repeat("q", 4096)})
	}
	tarball := buildTarball(t, entries)
	for _, tc := range []struct {
		name  string
		limit func(*service)
	}{
		{name: "compressed", limit: func(s *service) { s.maxDownload = 5 }},
		{name: "compressed mid-stream", limit: func(s *service) { s.maxDownload = int64(len(tarball) / 2) }},
		{name: "extracted mid-file", limit: func(s *service) { s.maxExtracted = 3000 }},
		{name: "extracted", limit: func(s *service) { s.maxExtracted = 6000 }},
		{name: "extracted between headers", limit: func(s *service) { s.maxExtracted = 512 }},
		{name: "held in memory", limit: func(s *service) { s.maxCollected = 10000 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{commitStatus: 404, tarball: tarball}
			s, done := gh.server(t)
			defer done()
			tc.limit(s)
			_, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil)
			if err == nil || !strings.Contains(err.Error(), "too large to import:") {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("extracted mid-manifest", func(t *testing.T) {
		gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, []tarEntry{{name: "r-main/.agentrq/plugin.json", body: strings.Repeat(" ", 4096)}})}
		s, done := gh.server(t)
		defer done()
		s.maxExtracted = 3000
		if _, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil); err == nil || !strings.Contains(err.Error(), "too large to import:") {
			t.Fatalf("got %v", err)
		}
	})
}

// A plugin manifest, in Muse's format as obra/superpowers ships it, says which
// directories are skills; a SKILL.md it does not list is not imported.
func TestFetch_PluginManifest(t *testing.T) {
	root := "superpowers-main/"
	muse := `{
  "schemaVersion": 1,
  "name": "superpowers",
  "compat": {"source": "native", "manifestDir": ".muse-plugin"},
  "capabilities": {
    "skills": [
      {"id": "brainstorming", "path": "skills/brainstorming/SKILL.md"},
      {"id": "by-dir", "path": "./skills/by-dir"},
      {"id": "gone", "path": "skills/gone/SKILL.md"}
    ],
    "commands": [],
    "hooks": [{"id": "session-start", "event": "SessionStart", "command": ["sh", "hooks/session-start"]}]
  }
}`
	base := []tarEntry{
		{name: root + ".muse-plugin/plugin.json", body: muse},
		{name: root + "skills/brainstorming/SKILL.md", body: skillMD("brainstorming", "Use before building.")},
		// No name in the frontmatter: the manifest's id names it.
		{name: root + "skills/by-dir/SKILL.md", body: "---\ndescription: Found by its directory.\n---\n"},
		{name: root + "skills/unlisted/SKILL.md", body: skillMD("unlisted", "Not in the manifest.")},
	}
	fetch := func(t *testing.T, url string, entries []tarEntry) *Result {
		t.Helper()
		gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, entries)}
		s, done := gh.server(t)
		defer done()
		res, err := s.Fetch(context.Background(), url, nil)
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		return res
	}

	t.Run("muse", func(t *testing.T) {
		res := fetch(t, "https://github.com/obra/superpowers/tree/main", base)
		if got := strings.Join(names(res.Skills), ","); got != "brainstorming,by-dir" {
			t.Errorf("skills: %s", got)
		}
		if len(res.Skipped) != 1 || res.Skipped[0].Name != "gone" || !strings.Contains(res.Skipped[0].Reason, "listed in .muse-plugin/plugin.json but is not in the repository") {
			t.Errorf("skipped: %+v", res.Skipped)
		}
	})
	t.Run("agentrq first", func(t *testing.T) {
		own := append([]tarEntry{{name: root + ".agentrq/plugin.json", body: `{"capabilities":{"skills":[{"id":"unlisted","path":"skills/unlisted/SKILL.md"}]}}`}}, base...)
		res := fetch(t, "https://github.com/obra/superpowers/tree/main", own)
		if got := strings.Join(names(res.Skills), ","); got != "unlisted" {
			t.Errorf("skills: %s", got)
		}
	})
	t.Run("a manifest with no skills is passed over", func(t *testing.T) {
		own := append([]tarEntry{{name: root + ".agentrq/plugin.json", body: `{"capabilities":{"skills":[]}}`}}, base...)
		res := fetch(t, "https://github.com/obra/superpowers/tree/main", own)
		if got := strings.Join(names(res.Skills), ","); got != "brainstorming,by-dir" {
			t.Errorf("skills: %s", got)
		}
	})
	t.Run("broken manifest falls back to looking", func(t *testing.T) {
		broken := []tarEntry{{name: root + ".muse-plugin/plugin.json", body: "{"}}
		res := fetch(t, "https://github.com/obra/superpowers/tree/main", append(broken, base[1:]...))
		if got := strings.Join(names(res.Skills), ","); got != "brainstorming,by-dir,unlisted" {
			t.Errorf("skills: %s", got)
		}
		if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "not valid JSON") {
			t.Errorf("skipped: %+v", res.Skipped)
		}
	})
	t.Run("a link to a directory still reads the root manifest", func(t *testing.T) {
		res := fetch(t, "https://github.com/obra/superpowers/tree/main/skills/by-dir", base)
		if len(res.Skills) != 1 || res.Skills[0].Name != "by-dir" || res.Skills[0].Dir != "skills/by-dir" {
			t.Errorf("skills: %+v", res.Skills)
		}
	})
	t.Run("a truncated manifest is a read error", func(t *testing.T) {
		var raw bytes.Buffer
		tw := tar.NewWriter(&raw)
		tw.WriteHeader(&tar.Header{Name: root + ".agentrq/plugin.json", Typeflag: tar.TypeReg, Size: 100, Mode: 0644})
		tw.Write([]byte("{"))
		gh := &fakeGitHub{commitStatus: 404, tarball: gzipped(raw.Bytes())}
		s, done := gh.server(t)
		defer done()
		if _, err := s.Fetch(context.Background(), "https://github.com/a/r/tree/main", nil); err == nil || !strings.Contains(err.Error(), "could not be read") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestContainsPath(t *testing.T) {
	for _, tc := range []struct {
		text, s string
		want    bool
	}{
		{"see a.md", "a.md", true},
		{"a.md", "a.md", true},
		{"end with a.md.", "a.md", true},
		{"(./a.md)", "a.md", true},
		{"xa.md", "a.md", false},
		{"a.md.bak", "a.md", false},
		{"dir/a.md", "a.md", false},
		{"../a.md", "a.md", false},
		{"x./a.md", "a.md", false},
		{"xa.md then a.md", "a.md", true},
		{"a.mdx", "a.md", false},
	} {
		if got := containsPath(tc.text, tc.s); got != tc.want {
			t.Errorf("containsPath(%q, %q) = %v, want %v", tc.text, tc.s, got, tc.want)
		}
	}
}

func TestRelPath(t *testing.T) {
	for _, tc := range []struct{ from, p, want string }{
		{"", "a/b.md", "a/b.md"},
		{"a", "a/b.md", "b.md"},
		{"a/c", "a/b.md", "../b.md"},
		{"x", "a/b.md", "../a/b.md"},
	} {
		if got := relPath(tc.from, tc.p); got != tc.want {
			t.Errorf("relPath(%q, %q) = %q, want %q", tc.from, tc.p, got, tc.want)
		}
	}
}

// Review regressions.

// Superpowers names files by where they sit in the repository —
// `skills/brainstorming/visual-companion.md` — and those are references too.
func TestFetch_KeepsFilesNamedByTheirRepositoryPath(t *testing.T) {
	root := "repo-main/"
	gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, []tarEntry{
		{name: root + "skills/brainstorming/SKILL.md", body: skillMD("brainstorming", "B.") + "See `skills/brainstorming/visual-companion.md`.\n"},
		{name: root + "skills/brainstorming/visual-companion.md", body: "vc"},
	})}
	s, done := gh.server(t)
	defer done()
	res, err := s.Fetch(context.Background(), "https://github.com/a/repo/tree/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || len(res.Skills[0].Files) != 2 {
		t.Fatalf("skills: %+v, skipped: %+v", res.Skills, res.Skipped)
	}
}

// A manifest that lists no skill under the linked directory says nothing
// about that directory, so its SKILL.md files are looked for as usual.
func TestFetch_ManifestWithNothingUnderTheLinkFallsBackToLooking(t *testing.T) {
	root := "repo-main/"
	gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, []tarEntry{
		{name: root + ".agentrq/plugin.json", body: `{"capabilities":{"skills":[{"id":"a","path":"skills/a/SKILL.md"}]}}`},
		{name: root + "skills/a/SKILL.md", body: skillMD("a", "A.")},
		{name: root + "extra/foo/SKILL.md", body: skillMD("foo", "Foo.")},
	})}
	s, done := gh.server(t)
	defer done()
	res, err := s.Fetch(context.Background(), "https://github.com/a/repo/tree/main/extra", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(res.Skills), ","); got != "foo" {
		t.Errorf("skills: %q, skipped: %+v", got, res.Skipped)
	}
}

// A SKILL.md too big even to read is reported against its own limit.
func TestFetch_OversizedSkillFileQuotesItsOwnLimit(t *testing.T) {
	gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, []tarEntry{
		{name: "repo-main/big/SKILL.md", body: strings.Repeat("x", skill.MaxSkillFileBytes+1)},
	})}
	s, done := gh.server(t)
	defer done()
	res, err := s.Fetch(context.Background(), "https://github.com/a/repo/tree/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "96 KiB") {
		t.Errorf("skipped: %+v", res.Skipped)
	}
}

// Every Markdown file in a skill's folder is part of the skill, referenced or
// not, except the files a repository keeps for its own sake.
func TestFetch_KeepsUnreferencedMarkdownButNotRepoMetaFiles(t *testing.T) {
	root := "repo-main/skills/tdd/"
	gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, []tarEntry{
		{name: root + "SKILL.md", body: skillMD("tdd", "T.") + "Nothing referenced here.\n"},
		{name: root + "test-pressure-1.md", body: "Run `helper.sh` first."},
		{name: root + "notes/CREATION-LOG.MD", body: "log"},
		{name: root + "helper.sh", body: "#!/bin/sh"},
		{name: root + "unreferenced.sh", body: "#!/bin/sh"},
		{name: root + "README.md", body: "repo readme"},
		{name: root + "notes/claude.md", body: "agent config"},
		{name: root + "Agents.md", body: "agent config"},
		{name: root + "CHANGELOG.md", body: "changes"},
	})}
	s, done := gh.server(t)
	defer done()
	res, err := s.Fetch(context.Background(), "https://github.com/a/repo/tree/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range res.Skills[0].Files {
		paths = append(paths, f.Path)
	}
	// helper.sh is kept because a kept Markdown file names it.
	sort.Strings(paths)
	if got := strings.Join(paths, ","); got != "SKILL.md,helper.sh,notes/CREATION-LOG.MD,test-pressure-1.md" {
		t.Errorf("kept %s", got)
	}
	skipped := map[string]bool{}
	for _, sk := range res.Skipped {
		skipped[sk.Path] = true
	}
	for _, p := range []string{"unreferenced.sh", "README.md", "notes/claude.md", "Agents.md", "CHANGELOG.md"} {
		if !skipped["skills/tdd/"+p] {
			t.Errorf("%s was not skipped: %+v", p, res.Skipped)
		}
	}
}

// A meta file SKILL.md names on purpose is still a reference.
func TestFetch_KeepsAMetaFileSkillMDReferences(t *testing.T) {
	gh := &fakeGitHub{commitStatus: 404, tarball: buildTarball(t, []tarEntry{
		{name: "repo-main/tdd/SKILL.md", body: skillMD("tdd", "T.") + "Read README.md first.\n"},
		{name: "repo-main/tdd/README.md", body: "the readme"},
	})}
	s, done := gh.server(t)
	defer done()
	res, err := s.Fetch(context.Background(), "https://github.com/a/repo/tree/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills[0].Files) != 2 {
		t.Errorf("files: %+v", res.Skills[0].Files)
	}
}
