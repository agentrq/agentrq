// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package skill holds the rules a workspace skill must follow, with no storage
// attached: what a SKILL.md must declare, what a skill or file may be called,
// and how large each file may be. Every path that writes a skill — the REST
// API, the MCP tools and the GitHub importer — checks against these, so the
// rules cannot drift between them.
package skill

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	// FileName is the one file every skill must have. Exactly this spelling:
	// it is what Claude Code, Antigravity and every published skill use.
	FileName = "SKILL.md"

	// MaxSkillFileBytes caps SKILL.md, which may be larger than its files.
	MaxSkillFileBytes = 96 * 1024
	// MaxSubFileBytes caps every other file in a skill.
	MaxSubFileBytes = 64 * 1024
	// MaxFiles caps the files in one skill, SKILL.md included.
	MaxFiles = 256
	// MaxNameLength is the longest skill name, in characters.
	MaxNameLength = 64
	// MaxDescriptionLength is the longest description, in characters.
	MaxDescriptionLength = 1024
	// MaxPathLength is the longest file path inside a skill, in characters.
	MaxPathLength = 255
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Frontmatter is what a SKILL.md declares about its skill.
type Frontmatter struct {
	Name        string
	Description string
}

// CanonicalName folds a skill name to the one spelling it is stored under and
// refuses anything that is still not a valid name after that.
func CanonicalName(name string) (string, error) {
	canonical := strings.ToLower(strings.TrimSpace(name))
	if canonical == "" {
		return "", fmt.Errorf("skill name is empty")
	}
	if n := utf8.RuneCountInString(canonical); n > MaxNameLength {
		return "", fmt.Errorf("skill name is %d characters; the limit is %d", n, MaxNameLength)
	}
	if !namePattern.MatchString(canonical) {
		return "", fmt.Errorf("skill name %q is not usable; names are lowercase letters and digits joined by single hyphens, like %q", name, "pr-reviewer")
	}
	return canonical, nil
}

// CleanPath checks a file path inside a skill and returns it unchanged.
//
// Refused rather than repaired: a path bent into shape files the content
// somewhere the writer did not choose, and the next read of it misses.
func CleanPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("file path is empty")
	}
	if n := utf8.RuneCountInString(p); n > MaxPathLength {
		return "", fmt.Errorf("file path is %d characters; the limit is %d", n, MaxPathLength)
	}
	if !utf8.ValidString(p) || strings.ContainsAny(p, "\\\x00") {
		return "", fmt.Errorf("file path %q is not usable; use forward slashes and printable characters", p)
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("file path %q is absolute; paths are relative to the skill, like %q", p, "references/guide.md")
	}
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "":
			return "", fmt.Errorf("file path %q has an empty segment", p)
		case seg == "." || seg == "..":
			return "", fmt.Errorf("file path %q may not contain %q", p, seg)
		case strings.HasPrefix(seg, "."):
			return "", fmt.Errorf("file path %q names a hidden file or directory", p)
		}
	}
	return p, nil
}

// CheckContent refuses a file that is too large for its place in the skill or
// is not text. Refused rather than truncated, so the writer can decide what to
// cut.
func CheckContent(p string, content []byte) error {
	limit := MaxSubFileBytes
	if p == FileName {
		limit = MaxSkillFileBytes
	}
	if len(content) > limit {
		return fmt.Errorf("%s is %d bytes; the limit is %d bytes (%d KiB)", p, len(content), limit, limit/1024)
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return fmt.Errorf("%s is not UTF-8 text; skills hold text files only", p)
	}
	return nil
}

// ParseSkillFile reads a SKILL.md's frontmatter and checks it.
//
// dirName is what the skill is called when the frontmatter names nothing: the
// directory it came from on import, or the name it is being saved under.
func ParseSkillFile(content []byte, dirName string) (Frontmatter, error) {
	if err := CheckContent(FileName, content); err != nil {
		return Frontmatter{}, err
	}

	text := strings.TrimPrefix(string(content), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Frontmatter{}, fmt.Errorf("%s must start with YAML frontmatter: a line of ---, then name and description, then another ---", FileName)
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Frontmatter{}, fmt.Errorf("%s frontmatter is not closed; end it with a line of ---", FileName)
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &raw); err != nil {
		return Frontmatter{}, fmt.Errorf("%s frontmatter is not valid YAML: %v", FileName, err)
	}

	var fm Frontmatter
	description, _ := raw["description"].(string)
	fm.Description = strings.TrimSpace(description)
	if fm.Description == "" {
		return Frontmatter{}, fmt.Errorf("%s frontmatter needs a description: say what the skill does and when to use it, since that is how an agent decides to load it", FileName)
	}
	if n := utf8.RuneCountInString(fm.Description); n > MaxDescriptionLength {
		return Frontmatter{}, fmt.Errorf("%s description is %d characters; the limit is %d", FileName, n, MaxDescriptionLength)
	}
	if strings.ContainsAny(fm.Description, "<>") {
		return Frontmatter{}, fmt.Errorf("%s description may not contain < or >", FileName)
	}

	name, _ := raw["name"].(string)
	if strings.TrimSpace(name) == "" {
		name = dirName
	}
	canonical, err := CanonicalName(name)
	if err != nil {
		return Frontmatter{}, err
	}
	fm.Name = canonical
	return fm, nil
}

// URI is how an agent addresses a file in a skill.
func URI(name, p string) string {
	return "skill://" + name + "/" + p
}

// ParseURI reads a skill URI into the skill's name and a file path within it.
// The scheme is matched without regard to case, and skill:/ and skill: are
// taken as well, since agents write them. A bare skill://<name> has path "".
func ParseURI(raw string) (name, p string, err error) {
	s := strings.TrimSpace(raw)
	scheme, rest, ok := strings.Cut(s, ":")
	if !ok || !strings.EqualFold(scheme, "skill") {
		return "", "", fmt.Errorf("%q is not a skill URI; write it as skill://<name>/<path>, like %q", raw, URI("pr-reviewer", FileName))
	}
	name, p, _ = strings.Cut(strings.TrimLeft(rest, "/"), "/")
	if name, err = CanonicalName(name); err != nil {
		return "", "", err
	}
	if p == "" {
		return name, "", nil
	}
	if _, err := CleanPath(p); err != nil {
		return "", "", err
	}
	return name, p, nil
}
