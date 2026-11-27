// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package skillimport reads skills out of a GitHub or GitLab repository: a
// public one, or a private one given a token that reads it.
//
// It fetches one tarball rather than walking the contents API: unauthenticated
// API calls are limited to 60 an hour per IP, which one repository of skills
// can exhaust on its own. And it never fetches a URL it was given — it parses
// the owner, repository and ref out of it and builds the API URLs itself, so
// an import cannot be pointed at anything but GitHub or the one GitLab host
// the server names. A token goes to the API alone, which serves a private
// repository's archive and files too.
package skillimport

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/skill"
)

const (
	// MaxImportBytes caps the skill content one import keeps.
	MaxImportBytes = 2 * 1024 * 1024

	maxDownloadBytes  = 20 * 1024 * 1024
	maxExtractedBytes = 64 * 1024 * 1024
	maxCollectedBytes = 16 * 1024 * 1024
	requestTimeout    = 30 * time.Second

	// MaxSelected caps the skills one import may choose.
	MaxSelected = 256
	// maxTreeBytes caps GitHub's listing of a repository's files.
	maxTreeBytes = 32 * 1024 * 1024
	// maxTreePages caps the pages of GitLab's file list read, 100 to a page.
	maxTreePages = 200
	// maxRawFiles caps the files one import reads one by one, and
	// rawParallel how many it reads at a time.
	maxRawFiles = 1024
	rawParallel = 8
)

var (
	// ErrInvalidURL is a URL that is not a GitHub or GitLab repository link.
	ErrInvalidURL = errors.New("invalid repository URL")
	// ErrNotFound is a repository or ref the host does not serve.
	ErrNotFound = errors.New("repository not found")

	segmentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	// hostPattern is a host name with an optional port, and nothing else.
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)
	pagePattern = regexp.MustCompile(`^[0-9]{1,6}$`)
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)

	// manifestPaths are the plugin manifests that list a repository's skills,
	// in the order they are looked for: AgentRQ's own, then Muse's, whose
	// format it shares.
	manifestPaths = []string{".agentrq/plugin.json", ".muse-plugin/plugin.json"}
)

type (
	// Source is what a repository URL names. On GitLab, Owner is the whole
	// namespace, subgroups included.
	Source struct {
		// GitLab is set for a link to the server's GitLab host.
		GitLab  bool
		Owner   string
		Repo    string
		Ref     string
		SubPath string
	}

	// File is one file of an imported skill, with its path relative to the
	// skill's directory.
	File struct {
		Path    string
		Content []byte
	}

	// Skill is one skill found in the repository, already validated.
	Skill struct {
		Name        string
		Description string
		// Dir is where the skill lives in the repository.
		Dir   string
		Files []File
	}

	// Skip is something the import left out, and why.
	Skip struct {
		Name   string
		Path   string
		Reason string
	}

	// Candidate is a skill a repository offers, found before any of its
	// files are read.
	Candidate struct {
		Name string
		// Path is the skill's directory in the repository, and what an
		// import names to choose it.
		Path       string
		SkillBytes int64
		// Reason says why it cannot be imported, when that is already known.
		Reason string
	}

	// Result is everything an import found.
	Result struct {
		// Host is "github" or "gitlab".
		Host    string
		Repo    string
		Ref     string
		Commit  string
		Skills  []Skill
		Skipped []Skip
		// Candidates are set instead of Skills when the repository is too
		// large to import whole and no skills were chosen: the import is
		// asked again, naming the ones wanted.
		Candidates []Candidate
	}

	// Service imports skills from GitHub and GitLab.
	Service interface {
		// Fetch imports the skills a link names; only, when not empty, is the
		// directories of the skills wanted. token, when set, reads the
		// repository through the host's API: a private one needs it.
		Fetch(ctx context.Context, rawURL string, only []string, token string) (*Result, error)
		// Parse reads a link the way Fetch would, without fetching anything.
		Parse(rawURL string) (Source, error)
	}

	service struct {
		client       *http.Client
		apiBase      string
		codeloadBase string
		rawBase      string
		// gitlabHost is the one GitLab links may name, and gitlabAPI its API.
		gitlabHost string
		gitlabAPI  string
		// gitlab is set on the copy that fetches from GitLab.
		gitlab bool
		// token is sent to the host's API, and nowhere else.
		token string
		// Byte budgets for one download: compressed, extracted, and held in
		// memory. Fields rather than constants so tests can reach them.
		maxDownload, maxExtracted, maxCollected int64
	}

	entry struct {
		path    string
		content []byte
		reason  string
		// unread is a file listed by GitHub's tree whose content has not
		// been downloaded; its size is what the tree says.
		unread bool
		size   int64
	}

	// tooLargeError is an archive over the download budgets.
	tooLargeError struct{ msg string }

	// manifest is the part of a plugin.json an import reads.
	manifest struct {
		Capabilities struct {
			Skills []struct {
				ID   string `json:"id"`
				Path string `json:"path"`
			} `json:"skills"`
		} `json:"capabilities"`
	}
)

// DefaultGitLabHost is the GitLab an importer reads when none is named.
const DefaultGitLabHost = "gitlab.com"

// New returns an importer that talks to github.com and to gitlabHost, a host
// name such as gitlab.com; empty is DefaultGitLabHost.
func New(gitlabHost string) Service {
	gitlabHost = GitLabHost(gitlabHost)
	return &service{
		client:       &http.Client{Timeout: requestTimeout},
		apiBase:      "https://api.github.com",
		codeloadBase: "https://codeload.github.com",
		rawBase:      "https://raw.githubusercontent.com",
		gitlabHost:   gitlabHost,
		gitlabAPI:    "https://" + gitlabHost + "/api/v4",
		maxDownload:  maxDownloadBytes,
		maxExtracted: maxExtractedBytes,
		maxCollected: maxCollectedBytes,
	}
}

// ParseURL reads the owner, repository, ref and directory out of a GitHub
// link. Accepted: https://github.com/<owner>/<repo>, optionally ending in .git,
// /tree/<ref> or /tree/<ref>/<path>. A ref containing a slash cannot be told
// apart from a path, so the first segment after /tree/ is always the ref.
func ParseURL(raw string) (Source, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil {
		return Source{}, fmt.Errorf("%w: use a link like https://github.com/owner/repo", ErrInvalidURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return Source{}, fmt.Errorf("%w: the link must name an owner and a repository", ErrInvalidURL)
	}
	src := Source{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	switch {
	case len(parts) == 2:
	case parts[2] == "tree" && len(parts) >= 4:
		src.Ref = parts[3]
		src.SubPath = strings.Join(parts[4:], "/")
	default:
		return Source{}, fmt.Errorf("%w: link to the repository, or to a directory in it with /tree/<ref>/<path>", ErrInvalidURL)
	}
	for _, seg := range append([]string{src.Owner, src.Repo, src.Ref}, strings.Split(src.SubPath, "/")...) {
		if seg != "" && (!segmentPattern.MatchString(seg) || seg == "." || seg == "..") {
			return Source{}, fmt.Errorf("%w: %q is not a valid part of a GitHub link", ErrInvalidURL, seg)
		}
	}
	if src.Owner == "" || src.Repo == "" {
		return Source{}, fmt.Errorf("%w: the link must name an owner and a repository", ErrInvalidURL)
	}
	return src, nil
}

// GitLabHost is the host an importer configured with host reads: host
// itself, lower case, or DefaultGitLabHost when it is empty.
func GitLabHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return DefaultGitLabHost
	}
	return host
}

// CheckGitLabHost refuses a GitLab host that is not a bare host name: the
// importer builds https://<host>/api/v4 from it.
func CheckGitLabHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	if !hostPattern.MatchString(host) {
		return fmt.Errorf("%q is not a host name, such as gitlab.com or gitlab.example.com:8443", host)
	}
	return nil
}

// ParseGitLabURL reads the namespace, project, ref and directory out of a
// link to a project on host. Accepted: https://<host>/<group>[/<subgroup>…]/<project>,
// optionally ending in .git, /-/tree/<ref> or /-/tree/<ref>/<path>. As on
// GitHub, the first segment after /-/tree/ is always the ref.
func ParseGitLabURL(raw, host string) (Source, error) {
	example := fmt.Errorf("%w: use a link like https://%s/group/project", ErrInvalidURL, host)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, host) || u.User != nil {
		return Source{}, example
	}
	project, rest, _ := strings.Cut(strings.Trim(u.Path, "/"), "/-/")
	parts := strings.Split(project, "/")
	if len(parts) < 2 {
		return Source{}, fmt.Errorf("%w: the link must name a group and a project", ErrInvalidURL)
	}
	src := Source{GitLab: true, Owner: strings.Join(parts[:len(parts)-1], "/"), Repo: strings.TrimSuffix(parts[len(parts)-1], ".git")}
	if rest != "" {
		tree := strings.Split(rest, "/")
		if tree[0] != "tree" || len(tree) < 2 {
			return Source{}, fmt.Errorf("%w: link to the project, or to a directory in it with /-/tree/<ref>/<path>", ErrInvalidURL)
		}
		src.Ref, src.SubPath = tree[1], strings.Join(tree[2:], "/")
	}
	for _, seg := range append(append(parts[:len(parts)-1:len(parts)-1], src.Repo, src.Ref), strings.Split(src.SubPath, "/")...) {
		if seg != "" && (!segmentPattern.MatchString(seg) || seg == "." || seg == "..") {
			return Source{}, fmt.Errorf("%w: %q is not a valid part of a GitLab link", ErrInvalidURL, seg)
		}
	}
	if src.Repo == "" || slices.Contains(parts[:len(parts)-1], "") {
		return Source{}, fmt.Errorf("%w: the link must name a group and a project", ErrInvalidURL)
	}
	return src, nil
}

// Parse reads a GitHub link, or a link to the importer's GitLab host.
func (s *service) Parse(rawURL string) (Source, error) {
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil && strings.EqualFold(u.Host, s.gitlabHost) {
		return ParseGitLabURL(rawURL, s.gitlabHost)
	}
	src, err := ParseURL(rawURL)
	if err != nil {
		return Source{}, fmt.Errorf("%w: use a link like https://github.com/owner/repo or https://%s/group/project", ErrInvalidURL, s.gitlabHost)
	}
	return src, nil
}

func (e tooLargeError) Error() string { return e.msg }

func (s *service) Fetch(ctx context.Context, rawURL string, only []string, token string) (*Result, error) {
	src, err := s.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if token != "" || src.GitLab {
		cp := *s
		cp.token, cp.gitlab = token, src.GitLab
		s = &cp
	}
	ctx, cancel := context.WithTimeout(ctx, 2*requestTimeout)
	defer cancel()

	repo := src.Owner + "/" + src.Repo
	if src.Ref == "" {
		if src.Ref, err = s.defaultBranch(ctx, repo); err != nil {
			return nil, err
		}
	}
	// Best effort: the commit only records what was imported, and a rate
	// limited API must not stop an import the tarball alone can serve.
	commit := s.commitSHA(ctx, repo, src.Ref)
	archiveRef := src.Ref
	if commit != "" {
		// Download the commit that was recorded, not whatever the ref points
		// at a moment later.
		archiveRef = commit
	}

	// A chosen few skills are read file by file: their repository was too
	// large for the archive, or they would not have been chosen.
	var entries []entry
	var manifests map[string][]byte
	if len(only) == 0 {
		entries, manifests, err = s.download(ctx, repo, archiveRef, src.SubPath)
	}
	var big tooLargeError
	tooLarge := errors.As(err, &big)
	if len(only) > 0 || tooLarge {
		if entries, manifests, err = s.listTree(ctx, repo, archiveRef, src.SubPath); err != nil {
			if tooLarge {
				return nil, fmt.Errorf("%v, and GitHub would not list its files to choose from: %w", big, err)
			}
			return nil, err
		}
		if len(only) == 0 {
			res := offer(entries, manifests, src)
			res.Host, res.Repo, res.Ref, res.Commit = s.hostKind(), repo, src.Ref, commit
			return res, nil
		}
		if err := s.readChosen(ctx, repo, archiveRef, entries, manifests, src, only); err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	res := collect(entries, manifests, src, only)
	res.Host, res.Repo, res.Ref, res.Commit = s.hostKind(), repo, src.Ref, commit
	return res, nil
}

// hostKind is the Result.Host of what s fetches.
func (s *service) hostKind() string {
	if s.gitlab {
		return "gitlab"
	}
	return "github"
}

// hostName is the host as a person names it, in errors.
func (s *service) hostName() string {
	if s.gitlab {
		return "GitLab"
	}
	return "GitHub"
}

// api is the base of the host's API, the one place a token is sent.
func (s *service) api() string {
	if s.gitlab {
		return s.gitlabAPI
	}
	return s.apiBase
}

// treeLink is how a link names a branch, in advice.
func (s *service) treeLink() string {
	if s.gitlab {
		return "/-/tree/<branch>"
	}
	return "/tree/<branch>"
}

// project is the API path of a repository: repos/<owner>/<repo> on GitHub,
// projects/<url-escaped path> on GitLab.
func (s *service) project(repo string) string {
	if s.gitlab {
		return s.gitlabAPI + "/projects/" + url.PathEscape(repo)
	}
	return s.apiBase + "/repos/" + repo
}

func (s *service) get(ctx context.Context, u, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "agentrq-skill-import")
	// Go drops the header on a redirect to another host, as from the API's
	// tarball to codeload, which is sent a token of its own in the URL.
	if s.token != "" && strings.HasPrefix(u, s.api()+"/") {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	return s.client.Do(req)
}

func (s *service) defaultBranch(ctx context.Context, repo string) (string, error) {
	resp, err := s.get(ctx, s.project(repo), "application/vnd.github+json")
	if err != nil {
		return "", fmt.Errorf("reach %s: %w", s.hostName(), err)
	}
	defer resp.Body.Close()
	// GitLab answers 401 for a private project asked about with no token.
	if resp.StatusCode == http.StatusNotFound || (s.gitlab && resp.StatusCode == http.StatusUnauthorized) {
		switch {
		case s.token != "" && s.gitlab:
			return "", fmt.Errorf("%w: project %s does not exist, or the access token cannot read it", ErrNotFound, repo)
		case s.token != "":
			return "", fmt.Errorf("%w: repository %s does not exist, or the GitHub App was not given it", ErrNotFound, repo)
		}
		return "", fmt.Errorf("%w: repository %s does not exist or is not public", ErrNotFound, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %d looking up %s; name a branch with %s to skip this lookup", s.hostName(), resp.StatusCode, repo, s.treeLink())
	}
	var body struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil || !segmentPattern.MatchString(body.DefaultBranch) {
		return "", fmt.Errorf("%s did not say which branch %s uses; name one with %s", s.hostName(), repo, s.treeLink())
	}
	return body.DefaultBranch, nil
}

func (s *service) commitSHA(ctx context.Context, repo, ref string) string {
	u, accept := s.project(repo)+"/commits/"+ref, "application/vnd.github.sha"
	if s.gitlab {
		u, accept = s.project(repo)+"/repository/commits/"+url.PathEscape(ref), ""
	}
	resp, err := s.get(ctx, u, accept)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var sha string
	if s.gitlab {
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body)
		sha = body.ID
	} else {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		sha = strings.TrimSpace(string(b))
	}
	if !shaPattern.MatchString(sha) {
		return ""
	}
	return sha
}

// errTooLarge is returned by limitedReader once its budget is spent.
var errTooLarge = errors.New("too large")

type limitedReader struct {
	r    io.Reader
	left int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, errTooLarge
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

// download reads the files under subPath out of the repository's tarball,
// and the plugin manifests at its root, wherever subPath points.
func (s *service) download(ctx context.Context, repo, ref, subPath string) ([]entry, map[string][]byte, error) {
	archive := s.codeloadBase + "/" + repo + "/tar.gz/" + ref
	switch {
	case s.gitlab:
		archive = s.project(repo) + "/repository/archive.tar.gz?sha=" + url.QueryEscape(ref)
	case s.token != "":
		archive = s.apiBase + "/repos/" + repo + "/tarball/" + ref
	}
	resp, err := s.get(ctx, archive, "")
	if err != nil {
		return nil, nil, fmt.Errorf("download from %s: %w", s.hostName(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, fmt.Errorf("%w: %s has no branch, tag or commit %q", ErrNotFound, repo, ref)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s answered %d downloading %s", s.hostName(), resp.StatusCode, repo)
	}

	// The whole archive is read whatever directory the link names, so a
	// narrower link does not help; say what the limits are instead.
	tooLarge := tooLargeError{fmt.Sprintf("%s is too large to import: its archive may be at most %d MB compressed and %d MB unpacked", repo, s.maxDownload>>20, s.maxExtracted>>20)}
	gz, err := gzip.NewReader(&limitedReader{r: resp.Body, left: s.maxDownload})
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return nil, nil, tooLarge
		}
		return nil, nil, fmt.Errorf("%s sent an archive that could not be read: %w", s.hostName(), err)
	}
	tr := tar.NewReader(&limitedReader{r: gz, left: s.maxExtracted})

	prefix := ""
	if subPath != "" {
		prefix = subPath + "/"
	}
	var entries []entry
	manifests := map[string][]byte{}
	var collected int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, errTooLarge) {
				return nil, nil, tooLarge
			}
			return nil, nil, fmt.Errorf("%s sent an archive that could not be read: %w", s.hostName(), err)
		}
		// Every entry sits under one <repo>-<ref>/ directory (GitLab's adds
		// the commit).
		_, name, ok := strings.Cut(hdr.Name, "/")
		isManifest := hdr.Typeflag == tar.TypeReg && hdr.Size <= skill.MaxSubFileBytes && slices.Contains(manifestPaths, name)
		if !ok || name == "" || (!strings.HasPrefix(name, prefix) && !isManifest) {
			continue
		}
		if isManifest {
			content, err := io.ReadAll(tr)
			if err != nil {
				if errors.Is(err, errTooLarge) {
					return nil, nil, tooLarge
				}
				return nil, nil, fmt.Errorf("%s sent an archive that could not be read: %w", s.hostName(), err)
			}
			manifests[name] = content
			continue
		}
		name = strings.TrimSuffix(name[len(prefix):], "/")
		switch hdr.Typeflag {
		case tar.TypeReg:
		case tar.TypeSymlink, tar.TypeLink:
			entries = append(entries, entry{path: name, reason: "links are not imported"})
			continue
		default:
			continue
		}
		if hdr.Size > fileLimit(name) {
			entries = append(entries, entry{path: name, reason: tooBig(hdr.Size, name)})
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			if errors.Is(err, errTooLarge) {
				return nil, nil, tooLarge
			}
			return nil, nil, fmt.Errorf("%s sent an archive that could not be read: %w", s.hostName(), err)
		}
		if collected += int64(len(content)); collected > s.maxCollected {
			return nil, nil, tooLarge
		}
		entries = append(entries, entry{path: name, content: content})
	}
	return entries, manifests, nil
}

// collect groups what was downloaded into skills, and each file belongs to
// the innermost skill above it. Only the skills in only are kept, when it
// names any.
func collect(entries []entry, manifests map[string][]byte, src Source, only []string) *Result {
	res := &Result{}
	dirs, ids, skips := skillDirs(entries, manifests, src)
	res.Skipped = append(res.Skipped, skips...)
	byDir := map[string][]entry{}
	for _, e := range entries {
		if d, ok := owningDir(e.path, dirs); ok {
			byDir[d] = append(byDir[d], e)
		}
	}

	ordered := make([]string, 0, len(dirs))
	for d := range dirs {
		ordered = append(ordered, d)
	}
	sort.Strings(ordered)

	chosen := map[string]bool{}
	for _, p := range only {
		chosen[p] = true
	}
	seen := map[string]bool{}
	budget := MaxImportBytes
	for _, d := range ordered {
		repoPath := path.Join(src.SubPath, d)
		if len(only) > 0 && !chosen[repoPath] {
			continue
		}
		delete(chosen, repoPath)
		sk, skips, reason := buildSkill(d, defaultName(d, ids, src), byDir[d], repoPath)
		switch {
		case reason != "":
		case seen[sk.Name]:
			reason = fmt.Sprintf("another skill in this import is already called %q", sk.Name)
		case len(sk.Files) > skill.MaxFiles:
			reason = fmt.Sprintf("has %d files; the limit is %d", len(sk.Files), skill.MaxFiles)
		case size(sk) > budget:
			reason = fmt.Sprintf("would take the import past its %d MB limit; import it on its own with /tree/<ref>/%s", MaxImportBytes/1024/1024, repoPath)
		}
		if reason != "" {
			res.Skipped = append(res.Skipped, Skip{Name: sk.Name, Path: repoPath, Reason: reason})
			continue
		}
		seen[sk.Name] = true
		budget -= size(sk)
		sk.Dir = repoPath
		res.Skills = append(res.Skills, sk)
		res.Skipped = append(res.Skipped, skips...)
	}
	for _, p := range only {
		if chosen[p] {
			delete(chosen, p)
			res.Skipped = append(res.Skipped, Skip{Path: p, Reason: "is not a skill in this repository"})
		}
	}
	return res
}

// defaultName is what the skill in directory d is called when its SKILL.md
// names nothing: its manifest id, else its directory's name.
func defaultName(d string, ids map[string]string, src Source) string {
	if id := ids[d]; id != "" {
		return id
	}
	if d != "" {
		return path.Base(d)
	}
	if src.SubPath != "" {
		return path.Base(src.SubPath)
	}
	return src.Repo
}

// offer lists the skills a listed repository holds, for an import to choose
// from, with what is already known to keep one out.
func offer(entries []entry, manifests map[string][]byte, src Source) *Result {
	dirs, ids, skips := skillDirs(entries, manifests, src)
	res := &Result{Skipped: skips, Candidates: []Candidate{}}
	files := map[string]entry{}
	for _, e := range entries {
		files[e.path] = e
	}
	for d := range dirs {
		f := files[path.Join(d, skill.FileName)]
		reason := f.reason
		if reason != "" {
			reason = skill.FileName + " " + reason
		}
		res.Candidates = append(res.Candidates, Candidate{
			Name: defaultName(d, ids, src), Path: path.Join(src.SubPath, d), SkillBytes: f.size, Reason: reason,
		})
	}
	sort.Slice(res.Candidates, func(i, j int) bool { return res.Candidates[i].Path < res.Candidates[j].Path })
	return res
}

// listTree lists the files under subPath, and reads the plugin manifests at
// the repository's root, from GitHub's tree of the ref: one API call, where
// the archive would download everything. Contents are read later, and only
// for the skills chosen.
func (s *service) listTree(ctx context.Context, repo, ref, subPath string) ([]entry, map[string][]byte, error) {
	list := s.githubTree
	if s.gitlab {
		list = s.gitlabTree
	}
	tree, err := list(ctx, repo, ref)
	if err != nil {
		return nil, nil, err
	}

	prefix := ""
	if subPath != "" {
		prefix = subPath + "/"
	}
	var entries []entry
	manifests := map[string][]byte{}
	for _, t := range tree {
		if t.Type != "blob" {
			continue
		}
		if slices.Contains(manifestPaths, t.Path) && t.Size <= skill.MaxSubFileBytes {
			content, err := s.readRaw(ctx, repo, ref, t.Path)
			if err != nil {
				return nil, nil, err
			}
			manifests[t.Path] = content
			continue
		}
		if !strings.HasPrefix(t.Path, prefix) {
			continue
		}
		e := entry{path: t.Path[len(prefix):], size: t.Size, unread: true}
		switch {
		case t.Mode == "120000":
			e.reason, e.unread = "links are not imported", false
		case t.Size > fileLimit(e.path):
			e.reason, e.unread = tooBig(t.Size, e.path), false
		}
		entries = append(entries, e)
	}
	return entries, manifests, nil
}

// listedFile is one path in a repository's file list.
type listedFile struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	// Size is 0 on GitLab, which does not list it: a file's size is then
	// learnt by reading it.
	Size int64 `json:"size"`
}

// githubTree lists a repository's files in one call.
func (s *service) githubTree(ctx context.Context, repo, ref string) ([]listedFile, error) {
	resp, err := s.get(ctx, s.apiBase+"/repos/"+repo+"/git/trees/"+ref+"?recursive=1", "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s has no branch, tag or commit %q", ErrNotFound, repo, ref)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d listing the files of %s", resp.StatusCode, repo)
	}
	var body struct {
		Truncated bool         `json:"truncated"`
		Tree      []listedFile `json:"tree"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTreeBytes)).Decode(&body); err != nil {
		return nil, fmt.Errorf("GitHub sent a file list for %s that could not be read: %w", repo, err)
	}
	if body.Truncated {
		return nil, fmt.Errorf("%s has more files than GitHub will list at once", repo)
	}
	return body.Tree, nil
}

// gitlabTree lists a project's files, a page of 100 at a time, up to
// maxTreePages pages.
func (s *service) gitlabTree(ctx context.Context, repo, ref string) ([]listedFile, error) {
	var tree []listedFile
	for page := "1"; page != ""; {
		if len(tree) >= maxTreePages*100 {
			return nil, fmt.Errorf("%s has more files than GitLab will list at once", repo)
		}
		u := s.project(repo) + "/repository/tree?recursive=true&per_page=100&ref=" + url.QueryEscape(ref) + "&page=" + url.QueryEscape(page)
		items, next, err := s.gitlabTreePage(ctx, repo, ref, u)
		if err != nil {
			return nil, err
		}
		tree, page = append(tree, items...), next
	}
	return tree, nil
}

func (s *service) gitlabTreePage(ctx context.Context, repo, ref, u string) ([]listedFile, string, error) {
	resp, err := s.get(ctx, u, "")
	if err != nil {
		return nil, "", fmt.Errorf("reach GitLab: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", fmt.Errorf("%w: %s has no branch, tag or commit %q", ErrNotFound, repo, ref)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GitLab answered %d listing the files of %s", resp.StatusCode, repo)
	}
	var items []listedFile
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTreeBytes)).Decode(&items); err != nil {
		return nil, "", fmt.Errorf("GitLab sent a file list for %s that could not be read: %w", repo, err)
	}
	next := resp.Header.Get("X-Next-Page")
	if next != "" && !pagePattern.MatchString(next) {
		next = ""
	}
	return items, next, nil
}

func tooBig(size int64, p string) string {
	return fmt.Sprintf("is %d bytes; the limit is %d KiB", size, fileLimit(p)/1024)
}

// readChosen downloads what the chosen skills keep: SKILL.md, their Markdown,
// and each file one of those references, and so on — the files collect would
// keep from the archive, so both ways import the same skill. The rest stay
// unread and are reported as unreferenced.
func (s *service) readChosen(ctx context.Context, repo, ref string, entries []entry, manifests map[string][]byte, src Source, only []string) error {
	dirs, _, _ := skillDirs(entries, manifests, src)
	byDir := map[string][]int{}
	for i, e := range entries {
		if d, ok := owningDir(e.path, dirs); ok && slices.Contains(only, path.Join(src.SubPath, d)) {
			byDir[d] = append(byDir[d], i)
		}
	}
	prefix := ""
	if src.SubPath != "" {
		prefix = src.SubPath + "/"
	}
	budget, count := s.maxCollected, 0
	read := func(batch []int) error {
		for _, i := range batch {
			if count++; count > maxRawFiles {
				return fmt.Errorf("the chosen skills have more than %d files to read; choose fewer", maxRawFiles)
			}
			if budget -= entries[i].size; budget < 0 {
				return fmt.Errorf("the chosen skills are larger than %d MB; choose fewer", s.maxCollected>>20)
			}
		}
		if err := s.readEntries(ctx, repo, ref, prefix, entries, batch); err != nil {
			return err
		}
		// A size the list did not give (GitLab's) is counted once read.
		for _, i := range batch {
			if budget -= int64(len(entries[i].content)) - entries[i].size; budget < 0 {
				return fmt.Errorf("the chosen skills are larger than %d MB; choose fewer", s.maxCollected>>20)
			}
		}
		return nil
	}

	for d, idx := range byDir {
		repoPath := path.Join(src.SubPath, d)
		rel := func(i int) string { return strings.TrimPrefix(strings.TrimPrefix(entries[i].path, d), "/") }
		reached := map[int]bool{}
		var batch []int
		for _, i := range idx {
			if r := rel(i); entries[i].unread && (r == skill.FileName || isSkillMarkdown(r)) {
				reached[i] = true
				batch = append(batch, i)
			}
		}
		for len(batch) > 0 {
			if err := read(batch); err != nil {
				return err
			}
			var next []int
			for _, from := range batch {
				text, dir := string(entries[from].content), dirOf(rel(from))
				for _, i := range idx {
					if !reached[i] && entries[i].unread && mentions(text, dir, rel(i), repoPath) {
						reached[i] = true
						next = append(next, i)
					}
				}
			}
			batch = next
		}
	}
	return nil
}

// readEntries downloads the entries at idx, a few at a time.
func (s *service) readEntries(ctx context.Context, repo, ref, prefix string, entries []entry, idx []int) error {
	var wg sync.WaitGroup
	errs := make([]error, len(idx))
	sem := make(chan struct{}, rawParallel)
	for n, i := range idx {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			content, err := s.readRaw(ctx, repo, ref, prefix+entries[i].path)
			entries[i].content, entries[i].unread, errs[n] = content, false, err
			if n := int64(len(content)); n > fileLimit(entries[i].path) {
				// Read only up to the limit: its true size is unknown.
				entries[i].content, entries[i].reason = nil, fmt.Sprintf("is over %d KiB, the limit", fileLimit(entries[i].path)/1024)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// readRaw downloads one file of the repository at ref.
func (s *service) readRaw(ctx context.Context, repo, ref, p string) ([]byte, error) {
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	u, accept := s.rawBase+"/"+repo+"/"+ref+"/"+strings.Join(segs, "/"), ""
	switch {
	case s.gitlab:
		u = s.project(repo) + "/repository/files/" + url.PathEscape(p) + "/raw?ref=" + url.QueryEscape(ref)
	case s.token != "":
		u, accept = s.apiBase+"/repos/"+repo+"/contents/"+strings.Join(segs, "/")+"?ref="+url.QueryEscape(ref), "application/vnd.github.raw"
	}
	resp, err := s.get(ctx, u, accept)
	if err != nil {
		return nil, fmt.Errorf("download from %s: %w", s.hostName(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d downloading %s from %s", s.hostName(), resp.StatusCode, p, repo)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, fileLimit(p)+1))
	if err != nil {
		return nil, fmt.Errorf("download %s from %s: %w", p, repo, err)
	}
	return content, nil
}

// skillDirs finds the skills' directories. The first plugin manifest that
// lists skills says where they are, with each entry's id as the skill's
// default name. Without one, every directory holding a SKILL.md is a skill.
func skillDirs(entries []entry, manifests map[string][]byte, src Source) (map[string]bool, map[string]string, []Skip) {
	dirs, ids := map[string]bool{}, map[string]string{}
	var skips []Skip
	have := map[string]bool{}
	for _, e := range entries {
		have[e.path] = true
	}
	for _, mp := range manifestPaths {
		raw, ok := manifests[mp]
		if !ok {
			continue
		}
		var m manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			skips = append(skips, Skip{Path: mp, Reason: "is not valid JSON, so it was ignored: " + err.Error()})
			continue
		}
		if len(m.Capabilities.Skills) == 0 {
			continue
		}
		prefix := ""
		if src.SubPath != "" {
			prefix = src.SubPath + "/"
		}
		for _, ms := range m.Capabilities.Skills {
			p := path.Clean("/" + ms.Path)[1:]
			if path.Base(p) != skill.FileName {
				p = path.Join(p, skill.FileName)
			}
			if !strings.HasPrefix(p, prefix) {
				// Outside the directory the link names.
				continue
			}
			rel := p[len(prefix):]
			if !have[rel] {
				skips = append(skips, Skip{Name: ms.ID, Path: p, Reason: "is listed in " + mp + " but is not in the repository"})
				continue
			}
			dirs[dirOf(rel)] = true
			ids[dirOf(rel)] = ms.ID
		}
		// A manifest with nothing under the linked directory says nothing
		// about it, so that directory is looked through as usual.
		if len(dirs) > 0 {
			return dirs, ids, skips
		}
	}
	for _, e := range entries {
		if path.Base(e.path) == skill.FileName {
			dirs[dirOf(e.path)] = true
		}
	}
	return dirs, ids, skips
}

// buildSkill validates one skill directory. A reason means the whole skill is
// left out; skips are single files left out of a skill that is kept.
func buildSkill(dir, dirName string, entries []entry, repoPath string) (Skill, []Skip, string) {
	var sk Skill
	var skips []Skip
	var skillFile *entry
	for i := range entries {
		e := entries[i]
		rel := strings.TrimPrefix(e.path, dir)
		rel = strings.TrimPrefix(rel, "/")
		if rel == skill.FileName {
			skillFile = &entries[i]
			continue
		}
		reason := e.reason
		if reason == "" {
			if _, err := skill.CleanPath(rel); err != nil {
				reason = err.Error()
			} else if err := skill.CheckContent(rel, e.content); err != nil {
				reason = err.Error()
			}
		}
		if reason != "" {
			skips = append(skips, Skip{Path: path.Join(repoPath, rel), Reason: reason})
			continue
		}
		sk.Files = append(sk.Files, File{Path: rel, Content: e.content})
	}

	if skillFile.reason != "" {
		return sk, nil, skill.FileName + " " + skillFile.reason
	}
	fm, err := skill.ParseSkillFile(skillFile.content, dirName)
	if err != nil {
		return sk, nil, err.Error()
	}
	sk.Name, sk.Description = fm.Name, fm.Description
	kept, unreferenced := keepReferenced(skillFile.content, sk.Files, repoPath)
	for _, f := range unreferenced {
		skips = append(skips, Skip{Path: path.Join(repoPath, f.Path), Reason: "is not referenced from " + skill.FileName + ", or from any file it references"})
	}
	sk.Files = append([]File{{Path: skill.FileName, Content: skillFile.content}}, kept...)
	for i := range skips {
		skips[i].Name = sk.Name
	}
	return sk, skips, ""
}

// keepReferenced keeps the skill's Markdown files and the files SKILL.md
// reaches: those it mentions, those a kept file mentions, and so on. A file
// is mentioned by its path from the skill root, its path from the mentioning
// file, or a folder holding it written with a trailing slash.
func keepReferenced(skillMD []byte, files []File, repoPath string) (kept, dropped []File) {
	type source struct{ dir, text string }
	reached := make([]bool, len(files))
	queue := []source{{"", string(skillMD)}}
	// Markdown in the skill's folder is part of the skill whether or not it
	// is referenced, and what it references is followed like the rest.
	for i, f := range files {
		if isSkillMarkdown(f.Path) {
			reached[i] = true
			queue = append(queue, source{dirOf(f.Path), string(f.Content)})
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for i, f := range files {
			if !reached[i] && mentions(cur.text, cur.dir, f.Path, repoPath) {
				reached[i] = true
				queue = append(queue, source{dirOf(f.Path), string(f.Content)})
			}
		}
	}
	for i, f := range files {
		if reached[i] {
			kept = append(kept, f)
		} else {
			dropped = append(dropped, f)
		}
	}
	return kept, dropped
}

// repoMetaFiles are Markdown files a repository keeps for itself or for the
// tools working on it, not for a skill's reader. They are kept only when
// something references them.
var repoMetaFiles = map[string]bool{
	"readme.md": true, "claude.md": true, "agents.md": true, "gemini.md": true,
	"changelog.md": true, "license.md": true, "contributing.md": true,
	"code_of_conduct.md": true, "security.md": true,
}

// isSkillMarkdown reports whether a file is Markdown that belongs to the skill
// unreferenced: any .md file except a repository's own meta files.
func isSkillMarkdown(p string) bool {
	base := strings.ToLower(path.Base(p))
	return strings.HasSuffix(base, ".md") && !repoMetaFiles[base]
}

// mentions reports whether text, in a file in directory from, names p —
// also by its path in the repository, the way superpowers names its files.
func mentions(text, from, p, repoPath string) bool {
	forms := []string{p, relPath(from, p)}
	if repoPath != "" {
		forms = append(forms, path.Join(repoPath, p))
	}
	for d := dirOf(p); d != ""; d = dirOf(d) {
		forms = append(forms, d+"/", relPath(from, d)+"/")
	}
	for _, f := range forms {
		if containsPath(text, f) {
			return true
		}
	}
	return false
}

// relPath is the path from directory from to p, both relative to the skill
// root.
func relPath(from, p string) string {
	if from == "" {
		return p
	}
	f, t := strings.Split(from, "/"), strings.Split(p, "/")
	i := 0
	for i < len(f) && i < len(t)-1 && f[i] == t[i] {
		i++
	}
	return strings.Repeat("../", len(f)-i) + strings.Join(t[i:], "/")
}

// containsPath reports whether s occurs in text as a whole path: not as
// part of a longer name, though a leading ./ or a full stop ending a
// sentence may touch it.
func containsPath(text, s string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], s)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(s)
		i = start + 1
		before := start == 0 || !isPathChar(text[start-1]) ||
			(text[start-1] == '/' && start >= 2 && text[start-2] == '.' && (start == 2 || !isPathChar(text[start-3])))
		after := end == len(text) || !isPathChar(text[end]) ||
			(text[end] == '.' && (end+1 == len(text) || !isPathChar(text[end+1])))
		if before && after {
			return true
		}
	}
}

func isPathChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_-./", c) >= 0
}

// fileLimit is the most a file may weigh: SKILL.md has its own, lower cap.
func fileLimit(p string) int64 {
	if path.Base(p) == skill.FileName {
		return int64(skill.MaxSkillFileBytes)
	}
	return skill.MaxSubFileBytes
}

func dirOf(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}

// owningDir is the innermost skill directory containing p.
func owningDir(p string, dirs map[string]bool) (string, bool) {
	for d := dirOf(p); ; d = dirOf(d) {
		if dirs[d] {
			return d, true
		}
		if d == "" {
			return "", false
		}
	}
}

func size(sk Skill) int {
	n := 0
	for _, f := range sk.Files {
		n += len(f.Content)
	}
	return n
}
