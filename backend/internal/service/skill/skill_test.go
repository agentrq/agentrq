// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package skill

import (
	"strings"
	"testing"
)

func TestCanonicalName(t *testing.T) {
	for _, tc := range []struct {
		in, want, err string
	}{
		{in: "  PR-Reviewer ", want: "pr-reviewer"},
		{in: "tdd2", want: "tdd2"},
		{in: "", err: "empty"},
		{in: strings.Repeat("a", 65), err: "65 characters"},
		{in: "pr_reviewer", err: "not usable"},
		{in: "pr--reviewer", err: "not usable"},
		{in: "-pr", err: "not usable"},
	} {
		got, err := CanonicalName(tc.in)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("CanonicalName(%q): want error containing %q, got %v", tc.in, tc.err, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("CanonicalName(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestCleanPath(t *testing.T) {
	for _, tc := range []struct {
		in, err string
	}{
		{in: "SKILL.md"},
		{in: "references/guide.md"},
		{in: "scripts/start-server.sh"},
		{in: "", err: "empty"},
		{in: strings.Repeat("a", 256), err: "256 characters"},
		{in: "a\\b.md", err: "forward slashes"},
		{in: "a\x00.md", err: "forward slashes"},
		{in: string([]byte{0xff}), err: "forward slashes"},
		{in: "/etc/passwd", err: "absolute"},
		{in: "a//b.md", err: "empty segment"},
		{in: "a/", err: "empty segment"},
		{in: "../x.md", err: `".."`},
		{in: "a/./b.md", err: `"."`},
		{in: ".env", err: "hidden"},
		{in: "a/.git/config", err: "hidden"},
	} {
		got, err := CleanPath(tc.in)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("CleanPath(%q): want error containing %q, got %v", tc.in, tc.err, err)
			}
			continue
		}
		if err != nil || got != tc.in {
			t.Errorf("CleanPath(%q) = %q, %v", tc.in, got, err)
		}
	}
}

// SKILL.md and the files beside it have different caps, because SKILL.md is
// what an agent reads on every match while the rest are loaded on demand.
func TestCheckContent_Limits(t *testing.T) {
	if err := CheckContent(FileName, filled(MaxSkillFileBytes)); err != nil {
		t.Errorf("SKILL.md at the limit: %v", err)
	}
	if err := CheckContent(FileName, filled(MaxSkillFileBytes+1)); err == nil || !strings.Contains(err.Error(), "96 KiB") {
		t.Errorf("SKILL.md over the limit: got %v", err)
	}
	if err := CheckContent("references/big.md", filled(MaxSubFileBytes)); err != nil {
		t.Errorf("sub file at the limit: %v", err)
	}
	if err := CheckContent("references/big.md", filled(MaxSubFileBytes+1)); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Errorf("sub file over the limit: got %v", err)
	}
	if err := CheckContent("logo.png", []byte{0x89, 'P', 'N', 'G', 0x00}); err == nil || !strings.Contains(err.Error(), "text") {
		t.Errorf("binary: got %v", err)
	}
	if err := CheckContent("bad.md", []byte{0xff, 0xfe}); err == nil {
		t.Error("invalid UTF-8 must be refused")
	}
}

func filled(n int) []byte {
	return []byte(strings.Repeat("a", n))
}

func TestParseSkillFile(t *testing.T) {
	for _, tc := range []struct {
		name, content, dir string
		wantName, wantDesc string
		err                string
	}{
		{
			name:     "name and description",
			content:  "---\nname: pr-reviewer\ndescription: Reviews pull requests.\n---\n# Goal\n",
			dir:      "whatever",
			wantName: "pr-reviewer", wantDesc: "Reviews pull requests.",
		},
		{
			name:     "name defaults to the directory",
			content:  "---\ndescription: \"Quoted: with a colon\"\nlicense: MIT\n---\n",
			dir:      "Git-Commit",
			wantName: "git-commit", wantDesc: "Quoted: with a colon",
		},
		{
			name:     "BOM and CRLF",
			content:  "\ufeff---\r\nname: crlf\r\ndescription: Works on Windows files.\r\n---\r\nbody",
			wantName: "crlf", wantDesc: "Works on Windows files.",
		},
		{name: "too large", content: "---\n" + strings.Repeat("a", MaxSkillFileBytes), err: "96 KiB"},
		{name: "no frontmatter", content: "# Just a heading\n", err: "must start with YAML frontmatter"},
		{name: "unclosed", content: "---\ndescription: x\n", err: "not closed"},
		{name: "bad yaml", content: "---\ndescription: [unclosed\n---\n", err: "not valid YAML"},
		{name: "no description", content: "---\nname: x\n---\n", err: "needs a description"},
		{name: "description not a string", content: "---\nname: x\ndescription: [a, b]\n---\n", err: "needs a description"},
		{name: "description too long", content: "---\nname: x\ndescription: " + strings.Repeat("d", 1025) + "\n---\n", err: "1025 characters"},
		{name: "description with a tag", content: "---\nname: x\ndescription: Use <b>this</b>\n---\n", err: "< or >"},
		{name: "bad name", content: "---\nname: Not Valid\ndescription: d\n---\n", err: "not usable"},
		{name: "no name anywhere", content: "---\ndescription: d\n---\n", dir: "", err: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := ParseSkillFile([]byte(tc.content), tc.dir)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("want error containing %q, got %v", tc.err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fm.Name != tc.wantName || fm.Description != tc.wantDesc {
				t.Errorf("got %+v", fm)
			}
		})
	}
}

func TestURI(t *testing.T) {
	if got := URI("brainstorming", "scripts/helper.js"); got != "skill://brainstorming/scripts/helper.js" {
		t.Errorf("URI: %q", got)
	}
}

func TestParseURI(t *testing.T) {
	for _, tc := range []struct {
		in, name, path, err string
	}{
		{in: "skill://systematic-debugging/SKILL.md", name: "systematic-debugging", path: "SKILL.md"},
		{in: " SKILL://Brainstorming/scripts/helper.js ", name: "brainstorming", path: "scripts/helper.js"},
		{in: "skill:/tdd/SKILL.md", name: "tdd", path: "SKILL.md"},
		{in: "skill:tdd", name: "tdd"},
		{in: "skill://tdd/", name: "tdd"},
		{in: "memory://tdd.md", err: "not a skill URI"},
		{in: "tdd/SKILL.md", err: "not a skill URI"},
		{in: "skill://", err: "empty"},
		{in: "skill://Not Valid/SKILL.md", err: "not usable"},
		{in: "skill://tdd/../x", err: "may not contain"},
	} {
		name, p, err := ParseURI(tc.in)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("ParseURI(%q): want error containing %q, got %v", tc.in, tc.err, err)
			}
			continue
		}
		if err != nil || name != tc.name || p != tc.path {
			t.Errorf("ParseURI(%q) = %q, %q, %v; want %q, %q", tc.in, name, p, err, tc.name, tc.path)
		}
	}
}

func TestWithName(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"replaces the name line", "---\nname: tdd\ndescription: d\n---\n# tdd\n", "---\nname: tdd-beta\ndescription: d\n---\n# tdd\n"},
		{"adds one when there is none", "---\ndescription: d\n---\nbody", "---\nname: tdd-beta\ndescription: d\n---\nbody"},
		{"keeps CRLF line ends", "---\r\nname: tdd\r\ndescription: d\r\n---\r\n", "---\r\nname: tdd-beta\r\ndescription: d\r\n---\r\n"},
		{"leaves an indented name alone", "---\nmeta:\n  name: x\n---\n", "---\nname: tdd-beta\nmeta:\n  name: x\n---\n"},
		{"drops a byte order mark", "\ufeff---\nname: tdd\n---\n", "---\nname: tdd-beta\n---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := WithName([]byte(tc.in), "tdd-beta")
			if err != nil || string(got) != tc.want {
				t.Errorf("WithName returned %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, in := range []string{"no frontmatter", "---\nname: tdd\n"} {
		if _, err := WithName([]byte(in), "x"); err == nil {
			t.Errorf("WithName(%q) returned no error, want one", in)
		}
	}
}
