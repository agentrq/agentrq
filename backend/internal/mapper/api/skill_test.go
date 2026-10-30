// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// Each skill route is mounted twice: under a workspace, and as the account's,
// which is workspace 0.
func TestFromHTTPRequestToSearchSkillsRequestEntity(t *testing.T) {
	checkRequest(t, "/workspaces/:id/skills", FromHTTPRequestToSearchSkillsRequestEntity, []requestCase[entity.SearchSkillsRequest]{
		{"a page of a workspace's matches", "/workspaces/" + b62(20) + "/skills?q=pdf&limit=5&offset=10", ``,
			&entity.SearchSkillsRequest{WorkspaceID: 20, Query: "pdf", Limit: 5, Offset: 10}},
		{"everything", "/workspaces/" + b62(20) + "/skills", ``, &entity.SearchSkillsRequest{WorkspaceID: 20}},
		// Left for the controller to refuse.
		{"a negative limit", "/workspaces/" + b62(20) + "/skills?limit=-1", ``, &entity.SearchSkillsRequest{WorkspaceID: 20, Limit: -1}},
		{"workspace 0", "/workspaces/0/skills", ``, nil},
	})
	checkRequest(t, "/skills", FromHTTPRequestToSearchSkillsRequestEntity, []requestCase[entity.SearchSkillsRequest]{
		{"the account's", "/skills?q=pdf", ``, &entity.SearchSkillsRequest{Query: "pdf"}},
	})
}

func TestFromHTTPRequestToNamedSkillRequestEntities(t *testing.T) {
	ws := "/workspaces/" + b62(20) + "/skills/"
	checkRequest(t, "/workspaces/:id/skills/:name", FromHTTPRequestToGetSkillRequestEntity, []requestCase[entity.GetSkillRequest]{
		{"a decoded name", ws + "pdf%20tools", ``, &entity.GetSkillRequest{WorkspaceID: 20, Name: "pdf tools"}},
		{"workspace 0", "/workspaces/0/skills/pdf", ``, nil},
		{"a broken escape", ws + "%zz", ``, nil},
	})
	checkRequest(t, "/skills/:name", FromHTTPRequestToGetSkillRequestEntity, []requestCase[entity.GetSkillRequest]{
		{"the account's", "/skills/pdf", ``, &entity.GetSkillRequest{Name: "pdf"}},
	})
	checkRequest(t, "/workspaces/:id/skills/:name", FromHTTPRequestToDeleteSkillRequestEntity, []requestCase[entity.DeleteSkillRequest]{
		{"a skill", ws + "pdf", ``, &entity.DeleteSkillRequest{WorkspaceID: 20, Name: "pdf"}},
		{"a broken escape", ws + "%zz", ``, nil},
	})
	checkRequest(t, "/workspaces/:id/skills/:name/files/*", FromHTTPRequestToGetSkillFileRequestEntity, []requestCase[entity.GetSkillFileRequest]{
		{"a nested, encoded path", ws + "pdf/files/scripts/fill%20form.py", ``,
			&entity.GetSkillFileRequest{WorkspaceID: 20, Name: "pdf", Path: "scripts/fill form.py"}},
		{"a broken escape in the name", ws + "%zz/files/a.py", ``, nil},
		{"a broken escape in the path", ws + "pdf/files/%zz", ``, nil},
	})
}

func TestFromHTTPRequestToSetSkillEnabledRequestEntity(t *testing.T) {
	ws := "/workspaces/" + b62(20) + "/skills/"
	checkRequest(t, "/workspaces/:id/skills/:name/enabled", FromHTTPRequestToSetSkillEnabledRequestEntity, []requestCase[entity.SetSkillEnabledRequest]{
		{"on", ws + "pdf/enabled", `{"enabled":true}`, &entity.SetSkillEnabledRequest{WorkspaceID: 20, Name: "pdf", Enabled: true}},
		{"off", ws + "pdf/enabled", `{"enabled":false}`, &entity.SetSkillEnabledRequest{WorkspaceID: 20, Name: "pdf"}},
		// An empty body must not read as off.
		{"not given", ws + "pdf/enabled", `{}`, nil},
		{"not JSON", ws + "pdf/enabled", `{`, nil},
		{"a broken escape", ws + "%zz/enabled", `{"enabled":true}`, nil},
	})
}

func TestFromHTTPRequestToImportSkillsRequestEntity(t *testing.T) {
	checkRequest(t, "/skills/import", FromHTTPRequestToImportSkillsRequestEntity, []requestCase[entity.ImportSkillsRequest]{
		{"chosen skills into workspaces", "/skills/import",
			`{"url":"https://example.com/r","overwrite":true,"skills":["pdf"],"workspaceIds":["` + b62(20) + `","` + b62(21) + `"]}`,
			&entity.ImportSkillsRequest{URL: "https://example.com/r", Overwrite: true, Skills: []string{"pdf"}, WorkspaceIDs: []int64{20, 21}}},
		{"everything, into no workspace", "/skills/import", `{"url":"https://example.com/r"}`,
			&entity.ImportSkillsRequest{URL: "https://example.com/r", WorkspaceIDs: []int64{}}},
		{"a workspace 0", "/skills/import", `{"url":"https://example.com/r","workspaceIds":["` + b62(20) + `","0"]}`, nil},
		{"no URL", "/skills/import", `{"overwrite":true}`, nil},
		{"not JSON", "/skills/import", `{`, nil},
	})
}

func TestSkillResponses(t *testing.T) {
	file := entity.SkillFile{Path: "SKILL.md", SizeBytes: 12, UpdatedAt: at, Content: "# PDF", URL: "https://cdn/skills/x"}
	wantFile := view.SkillFile{Path: "SKILL.md", SizeBytes: 12, UpdatedAt: at, Content: "# PDF", URL: "https://cdn/skills/x"}
	full := entity.Skill{ID: 7, CreatedAt: at, UpdatedAt: at.Add(1), Name: "pdf", Description: "d", SourceType: "github",
		SourceRepo: "o/r", SourceRef: "main", SourceCommit: "abc", SourcePath: "skills/pdf", LocallyModified: true,
		FileCount: 2, TotalBytes: 30, Enabled: true, WorkspaceIDs: []int64{20, 21}, WorkspaceEnabled: true,
		Files: []entity.SkillFile{file}}
	wantFull := view.Skill{ID: b62(7), CreatedAt: at, UpdatedAt: at.Add(1), Name: "pdf", Description: "d", SourceType: "github",
		SourceRepo: "o/r", SourceRef: "main", SourceCommit: "abc", SourcePath: "skills/pdf", LocallyModified: true,
		FileCount: 2, TotalBytes: 30, Enabled: true, WorkspaceIDs: []string{b62(20), b62(21)}, WorkspaceEnabled: true,
		Files: []view.SkillFile{wantFile}}
	// A skill on in no workspace still lists them, as an empty list.
	bare := entity.Skill{ID: 8, CreatedAt: at, UpdatedAt: at, Name: "bare", SourceType: "manual"}
	wantBare := view.Skill{ID: b62(8), CreatedAt: at, UpdatedAt: at, Name: "bare", SourceType: "manual", WorkspaceIDs: []string{}}

	checkResponse(t, FromGetSkillResponseEntityToHTTPResponse(&entity.GetSkillResponse{Skill: full}), view.GetSkillResponse{Skill: wantFull})
	checkResponse(t, FromSetSkillEnabledResponseEntityToHTTPResponse(&entity.SetSkillEnabledResponse{Skill: bare}), view.GetSkillResponse{Skill: wantBare})
	checkResponse(t, FromSearchSkillsResponseEntityToHTTPResponse(&entity.SearchSkillsResponse{Skills: []entity.Skill{full, bare}, Total: 9}),
		view.SearchSkillsResponse{Skills: []view.Skill{wantFull, wantBare}, Total: 9})
	checkResponse(t, FromGetSkillFileResponseEntityToHTTPResponse(&entity.GetSkillFileResponse{Skill: bare, File: file}),
		view.GetSkillFileResponse{Skill: wantBare, File: wantFile})

	if got := string(FromSearchSkillsResponseEntityToHTTPResponse(&entity.SearchSkillsResponse{})); got != `{"skills":[],"total":0}` {
		t.Errorf("no skills = %s", got)
	}
}

func TestFromImportSkillsResponseEntityToHTTPResponse(t *testing.T) {
	checkResponse(t, FromImportSkillsResponseEntityToHTTPResponse(&entity.ImportSkillsResponse{
		Imported:     []entity.Skill{{ID: 7, Name: "pdf", FileCount: 2, TotalBytes: 30, Description: "not in the summary"}},
		Skipped:      []entity.SkillImportSkip{{Name: "big", Path: "skills/big/video.mp4", Reason: "too large"}},
		SourceRepo:   "o/r",
		SourceRef:    "main",
		SourceCommit: "abc",
	}), view.ImportSkillsResponse{
		Imported:     []view.ImportedSkill{{Name: "pdf", FileCount: 2, TotalBytes: 30}},
		Skipped:      []view.SkippedSkillEntry{{Name: "big", Path: "skills/big/video.mp4", Reason: "too large"}},
		SourceRepo:   "o/r",
		SourceRef:    "main",
		SourceCommit: "abc",
	})

	// A repository too large to import whole offers candidates, and imports nothing.
	raw := FromImportSkillsResponseEntityToHTTPResponse(&entity.ImportSkillsResponse{
		Candidates: []entity.SkillImportCandidate{{Name: "pdf", Path: "skills/pdf", SkillBytes: 4096, Reason: "r"}, {Name: "doc", Path: "skills/doc"}},
		SourceRepo: "o/r",
	})
	checkResponse(t, raw, view.ImportSkillsResponse{
		Imported:   []view.ImportedSkill{},
		Skipped:    []view.SkippedSkillEntry{},
		Candidates: []view.SkillImportCandidate{{Name: "pdf", Path: "skills/pdf", SizeBytes: 4096, Reason: "r"}, {Name: "doc", Path: "skills/doc"}},
		SourceRepo: "o/r",
	})
	if want := `{"imported":[],"skipped":[],"sourceRepo":"","sourceRef":"","sourceCommit":""}`; string(FromImportSkillsResponseEntityToHTTPResponse(&entity.ImportSkillsResponse{})) != want {
		t.Errorf("an empty import = %s, want %s", FromImportSkillsResponseEntityToHTTPResponse(&entity.ImportSkillsResponse{}), want)
	}
}
