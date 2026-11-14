// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"regexp"
	"strings"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// panoHeadingLine is an ATX heading: its #s, then its text, without any
// closing #s.
var panoHeadingLine = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)

// panoSection is a heading and where its section is in the markdown's lines:
// from the heading's line up to, not including, last.
type panoSection struct {
	entity.PanoHeading
	first, last int
}

// panoSections is the markdown's headings outside fenced code, each with its
// section: up to the next heading of its level or a higher one, without the
// blank lines before it, which keep that heading apart from the section.
func panoSections(lines []string) []panoSection {
	var out []panoSection
	panoOutside(lines, func(i int) {
		if m := panoHeadingLine.FindStringSubmatch(lines[i]); m != nil && m[2] != "" {
			out = append(out, panoSection{PanoHeading: entity.PanoHeading{Level: len(m[1]), Text: m[2]}, first: i})
		}
	})
	for k := range out {
		last := len(lines)
		for _, next := range out[k+1:] {
			if next.Level <= out[k].Level {
				last = next.first
				break
			}
		}
		for last > out[k].first+1 && strings.TrimSpace(lines[last-1]) == "" {
			last--
		}
		out[k].last = last
		out[k].ChecklistDone, out[k].ChecklistTotal = panoChecklist(strings.Join(lines[out[k].first:last], "\n"))
	}
	return out
}

// PanoOutline is the markdown's headings, each with its section's checklist.
func PanoOutline(markdown string) []entity.PanoHeading {
	sections := panoSections(strings.Split(markdown, "\n"))
	out := make([]entity.PanoHeading, len(sections))
	for i, s := range sections {
		out[i] = s.PanoHeading
	}
	return out
}

// PanoSection is the section under the heading named, its heading line
// included, and the heading. The name matches a heading's text exactly, else
// ignoring case; #s before it are dropped. A name matching several headings,
// or none, is refused, naming the headings to choose from.
func PanoSection(markdown, name string) (entity.PanoHeading, string, error) {
	lines := strings.Split(markdown, "\n")
	s, err := panoFindSection(lines, name)
	if err != nil {
		return entity.PanoHeading{}, "", err
	}
	return s.PanoHeading, strings.Join(lines[s.first:s.last], "\n"), nil
}

// panoReplaceSection swaps the section under the heading named, its heading
// line included, for text.
func panoReplaceSection(markdown, name, text string) (string, error) {
	lines := strings.Split(markdown, "\n")
	s, err := panoFindSection(lines, name)
	if err != nil {
		return "", err
	}
	return strings.Join(append(append(lines[:s.first:s.first], text), lines[s.last:]...), "\n"), nil
}

func panoFindSection(lines []string, name string) (panoSection, error) {
	name = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(name), "#"))
	if name == "" {
		return panoSection{}, panoRefuse("section needs a heading's text")
	}
	sections := panoSections(lines)
	var found []panoSection
	for _, match := range []func(string) bool{
		func(t string) bool { return t == name },
		func(t string) bool { return strings.EqualFold(t, name) },
	} {
		for _, s := range sections {
			if match(s.Text) {
				found = append(found, s)
			}
		}
		if len(found) > 0 {
			break
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		return panoSection{}, panoRefuse("section %q matches %d headings:%s", name, len(found), panoHeadingList(found))
	case len(sections) == 0:
		return panoSection{}, panoRefuse("section %q: the doc has no headings", name)
	}
	return panoSection{}, panoRefuse("no heading is %q; the headings:%s", name, panoHeadingList(sections))
}

// panoHeadingList names the headings as the markdown writes them, one per
// line of the refusal.
func panoHeadingList(sections []panoSection) string {
	names := make([]string, len(sections))
	for i, s := range sections {
		names[i] = strings.Repeat("#", s.Level) + " " + s.Text
	}
	return "\n" + strings.Join(names, "\n")
}
