// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package skillimport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

// fakeGitLab serves the GitLab API endpoints the importer uses, for the
// project grp/sub/proj, and records what was asked of it. With token set, it
// answers 401 to a call without that token, as GitLab does for a private
// project.
type fakeGitLab struct {
	mu            sync.Mutex
	token         string
	projectStatus int
	projectBody   string
	commitStatus  int
	commitBody    string
	tarStatus     int
	tarball       []byte
	treeStatus    int
	treeBody      string
	// files are served one by one and listed by the tree, pageSize to a page.
	files    map[string]string
	extra    []listedFile
	pageSize int
	badPage  bool
	requests []string
}

const glProject = "/api/v4/projects/grp%2Fsub%2Fproj"

func (f *fakeGitLab) server(t *testing.T) *service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.RequestURI())
		f.mu.Unlock()
		if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := r.URL.EscapedPath()
		switch {
		case p == glProject:
			w.WriteHeader(orDefault(f.projectStatus))
			w.Write([]byte(f.projectBody))
		case strings.HasPrefix(p, glProject+"/repository/commits/"):
			w.WriteHeader(orDefault(f.commitStatus))
			w.Write([]byte(f.commitBody))
		case p == glProject+"/repository/archive.tar.gz":
			w.WriteHeader(orDefault(f.tarStatus))
			w.Write(f.tarball)
		case p == glProject+"/repository/tree":
			if r.URL.Query().Get("recursive") != "true" {
				t.Errorf("tree must be listed recursively, got %s", r.URL.RawQuery)
			}
			if f.treeBody != "" || f.treeStatus != 0 {
				w.WriteHeader(orDefault(f.treeStatus))
				w.Write([]byte(f.treeBody))
				return
			}
			f.treePage(t, w, r)
		case strings.HasPrefix(p, glProject+"/repository/files/") && strings.HasSuffix(p, "/raw"):
			name, _ := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(p, glProject+"/repository/files/"), "/raw"))
			body, ok := f.files[name]
			if !ok || r.URL.Query().Get("ref") == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(body))
		default:
			t.Errorf("unexpected request %s", p)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return &service{
		client: srv.Client(), apiBase: "https://api.github.invalid", gitlabHost: "gitlab.example", gitlabAPI: srv.URL + "/api/v4",
		maxDownload: maxDownloadBytes, maxExtracted: maxExtractedBytes, maxCollected: maxCollectedBytes,
		codeloadBase: "https://codeload.invalid", rawBase: "https://raw.invalid/" + u.Host,
	}
}

func (f *fakeGitLab) treePage(t *testing.T, w http.ResponseWriter, r *http.Request) {
	var items []listedFile
	for p := range f.files {
		items = append(items, listedFile{Path: p, Mode: "100644", Type: "blob"})
	}
	items = append(items, f.extra...)
	slices.SortFunc(items, func(a, b listedFile) int { return strings.Compare(a.Path, b.Path) })
	size := f.pageSize
	if size == 0 {
		size = len(items) + 1
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	start := min((page-1)*size, len(items))
	end := min(start+size, len(items))
	if end < len(items) {
		w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
	}
	if f.badPage {
		w.Header().Set("X-Next-Page", "../x")
	}
	b, err := json.Marshal(items[start:end])
	if err != nil {
		t.Fatal(err)
	}
	w.Write(b)
}

func TestNew_GitLabHost(t *testing.T) {
	s := New("").(*service)
	if s.gitlabHost != "gitlab.com" || s.gitlabAPI != "https://gitlab.com/api/v4" {
		t.Errorf("New with no host must read gitlab.com, got %q %q", s.gitlabHost, s.gitlabAPI)
	}
	s = New(" Git.Example.com:8443 ").(*service)
	if s.gitlabHost != "git.example.com:8443" || s.gitlabAPI != "https://git.example.com:8443/api/v4" {
		t.Errorf("New must read the host it is given, got %q %q", s.gitlabHost, s.gitlabAPI)
	}
}

func TestCheckGitLabHost(t *testing.T) {
	for _, ok := range []string{"", "gitlab.com", " gitlab.example.com:8443 ", "localhost"} {
		if err := CheckGitLabHost(ok); err != nil {
			t.Errorf("CheckGitLabHost(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"https://gitlab.com", "gitlab.com/api", "user@gitlab.com", "gitlab.com:", "-gitlab.com", "a b"} {
		if err := CheckGitLabHost(bad); err == nil {
			t.Errorf("CheckGitLabHost(%q) must refuse it", bad)
		}
	}
}

func TestParseGitLabURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Source
		err  bool
	}{
		{in: "https://gitlab.com/grp/proj", want: Source{GitLab: true, Owner: "grp", Repo: "proj"}},
		{in: " https://GitLab.com/grp/sub/proj.git/ ", want: Source{GitLab: true, Owner: "grp/sub", Repo: "proj"}},
		{in: "https://gitlab.com/grp/sub/proj/-/tree/main", want: Source{GitLab: true, Owner: "grp/sub", Repo: "proj", Ref: "main"}},
		{in: "https://gitlab.com/grp/proj/-/tree/v1.2/skills/tdd", want: Source{GitLab: true, Owner: "grp", Repo: "proj", Ref: "v1.2", SubPath: "skills/tdd"}},
		{in: "http://gitlab.com/grp/proj", err: true},
		{in: "https://github.com/grp/proj", err: true},
		{in: "https://user:pw@gitlab.com/grp/proj", err: true},
		{in: "https://gitlab.com/grp", err: true},
		{in: "https://gitlab.com/grp//proj", err: true},
		{in: "https://gitlab.com/grp/.git", err: true},
		{in: "https://gitlab.com/grp/proj/-/blob/main/README.md", err: true},
		{in: "https://gitlab.com/grp/proj/-/tree", err: true},
		{in: "https://gitlab.com/grp/pr%20oj", err: true},
		{in: "https://gitlab.com/grp/proj/-/tree/main/../../x", err: true},
		{in: "https://gitlab.com/../proj", err: true},
		{in: "://bad", err: true},
	} {
		got, err := ParseGitLabURL(tc.in, "gitlab.com")
		if tc.err {
			if !errors.Is(err, ErrInvalidURL) {
				t.Errorf("ParseGitLabURL(%q): want ErrInvalidURL, got %+v, %v", tc.in, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseGitLabURL(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
}

func TestParse_PicksTheHost(t *testing.T) {
	s := New("gitlab.example").(*service)
	if src, err := s.Parse("https://gitlab.example/g/p"); err != nil || !src.GitLab {
		t.Errorf("a link to the GitLab host is a GitLab source: %+v, %v", src, err)
	}
	if src, err := s.Parse("https://github.com/o/r"); err != nil || src.GitLab {
		t.Errorf("a GitHub link is a GitHub source: %+v, %v", src, err)
	}
	// Another GitLab is not reachable: only the one host the server names.
	_, err := s.Parse("https://gitlab.com/g/p")
	if !errors.Is(err, ErrInvalidURL) || !strings.Contains(err.Error(), "https://gitlab.example/group/project") {
		t.Errorf("another host must be refused, naming the ones accepted: %v", err)
	}
}

func TestFetch_GitLabArchive(t *testing.T) {
	root := "proj-main-" + testSHA + "/"
	gl := &fakeGitLab{
		projectBody: `{"default_branch":"main"}`,
		commitBody:  `{"id":"` + testSHA + `"}`,
		tarball: buildTarball(t, []tarEntry{
			{name: root + "skills/tdd/SKILL.md", body: skillMD("tdd", "Use when testing.") + "See notes.md.\n"},
			{name: root + "skills/tdd/notes.md", body: "notes"},
			{name: root + "skills/tdd/unused.sh", body: "x"},
		}),
	}
	s := gl.server(t)
	res, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj", nil, "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Host != "gitlab" || res.Repo != "grp/sub/proj" || res.Ref != "main" || res.Commit != testSHA {
		t.Errorf("source: %+v", res)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "tdd" || len(res.Skills[0].Files) != 2 || res.Skills[0].Dir != "skills/tdd" {
		t.Fatalf("skills: %+v", res.Skills)
	}
	want := glProject + "/repository/archive.tar.gz?sha=" + testSHA
	if last := gl.requests[len(gl.requests)-1]; last != want {
		t.Errorf("archive request %s, want %s", last, want)
	}
}

// A ref with no commit lookup still imports, from the ref itself; an answer
// that is no commit is ignored.
func TestFetch_GitLabCommitLookupFails(t *testing.T) {
	for _, body := range []string{"", `{"id":"nope"}`, "{"} {
		gl := &fakeGitLab{commitBody: body, tarball: gzipped(nil)}
		s := gl.server(t)
		res, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/v1", nil, "")
		if err != nil || res.Commit != "" || res.Ref != "v1" {
			t.Fatalf("body %q: %+v, %v", body, res, err)
		}
		if last := gl.requests[len(gl.requests)-1]; last != glProject+"/repository/archive.tar.gz?sha=v1" {
			t.Errorf("archive request %s", last)
		}
	}
}

// A private project is read with the token, sent as a bearer token to the
// GitLab API and nowhere else.
func TestFetch_GitLabPrivateWithToken(t *testing.T) {
	root := "proj-main/"
	gl := &fakeGitLab{
		token:       "glpat-secret",
		projectBody: `{"default_branch":"main"}`,
		commitBody:  `{"id":"` + testSHA + `"}`,
		tarball:     buildTarball(t, []tarEntry{{name: root + "SKILL.md", body: skillMD("only", "d")}}),
	}
	s := gl.server(t)
	res, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj", nil, "glpat-secret")
	if err != nil || len(res.Skills) != 1 || res.Skills[0].Name != "only" {
		t.Fatalf("Fetch: %+v, %v", res, err)
	}
	if s.token != "" || s.gitlab {
		t.Error("Fetch must not keep the token or the host on the shared importer")
	}

	// Without it, GitLab answers 401, which is a project not found.
	_, err = s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj", nil, "")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "is not public") {
		t.Errorf("no token: %v", err)
	}
	_, err = s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj", nil, "glpat-wrong")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "the access token cannot read it") {
		t.Errorf("wrong token: %v", err)
	}
}

func TestFetch_GitLabErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		gl   *fakeGitLab
		want string
		is   error
	}{
		{name: "project missing", url: "https://gitlab.example/grp/sub/proj", gl: &fakeGitLab{projectStatus: 404}, is: ErrNotFound},
		{name: "project lookup fails", url: "https://gitlab.example/grp/sub/proj", gl: &fakeGitLab{projectStatus: 500}, want: "GitLab answered 500 looking up grp/sub/proj; name a branch with /-/tree/<branch>"},
		{name: "project body garbage", url: "https://gitlab.example/grp/sub/proj", gl: &fakeGitLab{projectBody: "{"}, want: "GitLab did not say which branch"},
		{name: "ref missing", url: "https://gitlab.example/grp/sub/proj/-/tree/nope", gl: &fakeGitLab{commitStatus: 404, tarStatus: 404}, is: ErrNotFound},
		{name: "download fails", url: "https://gitlab.example/grp/sub/proj/-/tree/main", gl: &fakeGitLab{commitStatus: 404, tarStatus: 500}, want: "GitLab answered 500 downloading"},
		{name: "not gzip", url: "https://gitlab.example/grp/sub/proj/-/tree/main", gl: &fakeGitLab{commitStatus: 404, tarball: []byte("plain")}, want: "GitLab sent an archive that could not be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gl := tc.gl
			s := gl.server(t)
			res, err := s.Fetch(context.Background(), tc.url, nil, "")
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

// A project too large for the archive is listed page by page, and its skills
// offered; GitLab gives no sizes, so none is known yet.
func TestFetch_GitLabTooLargeOffersItsSkills(t *testing.T) {
	gl := &fakeGitLab{
		commitBody: `{"id":"` + testSHA + `"}`,
		tarball:    []byte(strings.Repeat("x", 64)),
		files: map[string]string{
			".agentrq/plugin.json": `{"capabilities":{"skills":[{"id":"alpha","path":"skills/a"}]}}`,
			"skills/a/SKILL.md":    skillMD("alpha", "d"),
			"skills/b/SKILL.md":    skillMD("beta", "d"),
		},
		extra:    []listedFile{{Path: "skills", Mode: "040000", Type: "tree"}, {Path: "skills/a/link.md", Mode: "120000", Type: "blob"}},
		pageSize: 2,
	}
	s := gl.server(t)
	s.maxDownload = 5
	res, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", nil, "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].Name != "alpha" || res.Candidates[0].Path != "skills/a" || res.Candidates[0].SkillBytes != 0 {
		t.Errorf("candidates: %+v", res.Candidates)
	}
	pages := 0
	for _, r := range gl.requests {
		if strings.Contains(r, "/repository/tree?") {
			pages++
		}
	}
	if pages != 3 {
		t.Errorf("read %d pages of the file list, want 3: %v", pages, gl.requests)
	}
}

// A chosen skill is read file by file; a file GitLab gave no size for is
// refused once it turns out too large.
func TestFetch_GitLabChosenReadsFiles(t *testing.T) {
	gl := &fakeGitLab{
		commitBody: `{"id":"` + testSHA + `"}`,
		files: map[string]string{
			"skills/a/SKILL.md":    skillMD("alpha", "d") + "See docs/ref.md and big.md.\n",
			"skills/a/docs/ref.md": "ref",
			"skills/a/big.md":      strings.Repeat("z", skill.MaxSubFileBytes+1),
			"skills/b/SKILL.md":    skillMD("beta", "d"),
		},
	}
	s := gl.server(t)
	res, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", []string{"skills/a"}, "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].Name != "alpha" {
		t.Fatalf("skills: %+v", res.Skills)
	}
	var paths []string
	for _, f := range res.Skills[0].Files {
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, ",") != "SKILL.md,docs/ref.md" {
		t.Errorf("files: %v", paths)
	}
	var skipped bool
	for _, sk := range res.Skipped {
		if sk.Path == "skills/a/big.md" && strings.Contains(sk.Reason, "is over 64 KiB") {
			skipped = true
		}
	}
	if !skipped {
		t.Errorf("big.md must be skipped as too large: %+v", res.Skipped)
	}
	for _, r := range gl.requests {
		if strings.Contains(r, "skills%2Fb") {
			t.Errorf("a skill not chosen was read: %s", r)
		}
	}
	if want := "/repository/files/skills%2Fa%2Fdocs%2Fref.md/raw?ref=" + testSHA; !slices.ContainsFunc(gl.requests, func(r string) bool { return strings.HasSuffix(r, want) }) {
		t.Errorf("a file is read by its escaped path at the commit: %v", gl.requests)
	}
}

// The budget counts what GitLab's list did not give: the files once read.
func TestFetch_GitLabChosenOverBudget(t *testing.T) {
	gl := &fakeGitLab{
		commitBody: `{"id":"` + testSHA + `"}`,
		files:      map[string]string{"skills/a/SKILL.md": skillMD("alpha", "d") + strings.Repeat("x", 100)},
	}
	s := gl.server(t)
	s.maxCollected = 50
	_, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", []string{"skills/a"}, "")
	if err == nil || !strings.Contains(err.Error(), "choose fewer") {
		t.Errorf("want the budget refusal, got %v", err)
	}
}

func TestFetch_GitLabTreeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		gl   *fakeGitLab
		want string
		is   error
	}{
		{name: "ref missing", gl: &fakeGitLab{treeStatus: 404}, is: ErrNotFound},
		{name: "list fails", gl: &fakeGitLab{treeStatus: 500}, want: "GitLab answered 500 listing the files"},
		{name: "list garbage", gl: &fakeGitLab{treeBody: "{"}, want: "GitLab sent a file list"},
		{name: "a file is gone", gl: &fakeGitLab{extra: []listedFile{{Path: "skills/a/SKILL.md", Type: "blob"}}}, want: "GitLab answered 404 downloading skills/a/SKILL.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gl := tc.gl
			gl.commitBody = `{"id":"` + testSHA + `"}`
			s := gl.server(t)
			_, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", []string{"skills/a"}, "")
			if err == nil {
				t.Fatal("want an error")
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

// A next page that is not a page number ends the list; a list longer than
// the cap is refused rather than read forever.
func TestFetch_GitLabTreePages(t *testing.T) {
	gl := &fakeGitLab{
		commitBody: `{"id":"` + testSHA + `"}`,
		files:      map[string]string{"skills/a/SKILL.md": skillMD("alpha", "d")},
		badPage:    true,
	}
	s := gl.server(t)
	if _, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", []string{"skills/a"}, ""); err != nil {
		t.Errorf("a bad next page must end the list: %v", err)
	}

	files := map[string]string{}
	for i := range maxTreePages*100 + 1 {
		files["f"+strconv.Itoa(i)] = ""
	}
	gl = &fakeGitLab{commitBody: `{"id":"` + testSHA + `"}`, files: files, pageSize: 100}
	s = gl.server(t)
	_, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", []string{"skills/a"}, "")
	if err == nil || !strings.Contains(err.Error(), "more files than GitLab will list") {
		t.Errorf("want the page cap, got %v", err)
	}
}

func TestFetch_GitLabUnreachable(t *testing.T) {
	gl := &fakeGitLab{}
	s := gl.server(t)
	s.gitlabAPI = "http://127.0.0.1:1/api/v4"
	if _, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj", nil, ""); err == nil || !strings.Contains(err.Error(), "reach GitLab") {
		t.Errorf("want reach GitLab, got %v", err)
	}
	if _, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", nil, ""); err == nil || !strings.Contains(err.Error(), "download from GitLab") {
		t.Errorf("want download from GitLab, got %v", err)
	}
	if _, err := s.Fetch(context.Background(), "https://gitlab.example/grp/sub/proj/-/tree/main", []string{"x"}, ""); err == nil || !strings.Contains(err.Error(), "reach GitLab") {
		t.Errorf("want reach GitLab from the list, got %v", err)
	}
}
