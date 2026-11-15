// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
)

// must is a GET as user that must answer 200, decoded.
func (m *teamMode) must(t *testing.T, user, path string) map[string]any {
	t.Helper()
	status, body := m.get(t, user, path)
	if status != http.StatusOK {
		t.Fatalf("%s answered %d %v", path, status, body)
	}
	return body
}

// teamVersion is team 10's version now, as Alice reads it.
func (m *teamMode) teamVersion(t *testing.T) int {
	t.Helper()
	_, body := m.get(t, aliceID, "/teams/"+teamTen)
	v, _ := body["version"].(float64)
	return int(v)
}

// pano: Alice shares one doc of her Pano with team 10; Bob reads it, and
// its files on the team's route, until she unshares it; nobody else ever
// does, and her other docs stay hers.
func (m *teamMode) pano(t *testing.T) {
	ctx := context.Background()
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	saved, err := m.crud.SavePanoDoc(ctx, entity.SavePanoDocRequest{UserID: aliceID, Title: "Plan", Markdown: "# Plan\n\n![chart](chart.png)",
		Files: []entity.PanoUpload{{Filename: "chart.png", MimeType: "image/png", Data: b64("png bytes")}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := monoflake.ID(saved.Doc.ID).String()
	if _, err := m.crud.AttachPanoFiles(ctx, entity.AttachPanoFilesRequest{UserID: aliceID, ID: saved.Doc.ID,
		Files: []entity.PanoUpload{{Filename: "page.html", MimeType: "text/html", Data: b64("<script>alert(1)</script>")}}}); err != nil {
		t.Fatal(err)
	}
	private, err := m.crud.SavePanoDoc(ctx, entity.SavePanoDocRequest{UserID: aliceID, Title: "Mine", Markdown: "![secret](secret.png)",
		Files: []entity.PanoUpload{{Filename: "secret.png", MimeType: "image/png", Data: b64("secret bytes")}}})
	if err != nil {
		t.Fatal(err)
	}
	mine := monoflake.ID(private.Doc.ID).String()
	secretFile := monoflake.ID(private.Doc.Files[0].ID).String()
	// Dave's own doc, shared with his own team: no business of team 10.
	daves, err := m.crud.SavePanoDoc(ctx, entity.SavePanoDocRequest{UserID: daveID, Title: "Dave's", Markdown: "dave"})
	if err != nil {
		t.Fatal(err)
	}
	daveDoc := monoflake.ID(daves.Doc.ID).String()
	teamThirty := monoflake.ID(30).String()
	_, body := m.get(t, daveID, "/teams/"+teamThirty)
	status, body := call(t, m.app, daveID, http.MethodPut, "/pano/"+daveDoc+"/shares/"+teamThirty, fmt.Sprintf(`{"version":%d}`, int(body["version"].(float64))))
	expectStatus(t, "Dave sharing his doc with his own team", status, http.StatusOK, body)

	status, body = m.get(t, bobID, "/teams/"+teamTen+"/pano")
	if status != http.StatusOK || len(list(body, "docs", "id")) != 0 {
		t.Errorf("before any share, Bob's team Pano answered %d %v", status, body)
	}
	status, body = m.get(t, aliceID, "/pano/"+doc+"/shares")
	if shares := list(body, "shares", "teamId"); status != http.StatusOK || len(shares) != 0 {
		t.Errorf("Plan's shares before sharing: %d %v", status, body)
	}

	v := m.teamVersion(t)
	status, body = call(t, m.app, aliceID, http.MethodPut, "/pano/"+doc+"/shares/"+teamTen, fmt.Sprintf(`{"version":%d}`, v))
	if status != http.StatusOK || body["teamName"] != "core" || body["docId"] != doc || body["version"] != float64(v+1) {
		t.Errorf("Alice sharing Plan answered %d %v", status, body)
	}
	status, body = call(t, m.app, bobID, http.MethodPut, "/pano/"+doc+"/shares/"+teamTen, fmt.Sprintf(`{"version":%d}`, v+1))
	expectStatus(t, "Bob sharing Alice's doc", status, http.StatusNotFound, body)
	status, body = call(t, m.app, aliceID, http.MethodPut, "/pano/"+doc+"/shares/"+teamTen, `{"version":0}`)
	expectStatus(t, "sharing with no version", status, http.StatusBadRequest, body)
	status, body = m.get(t, aliceID, "/pano/"+doc+"/shares")
	if shares := list(body, "shares", "teamName"); status != http.StatusOK || !sameList(shares, "core") {
		t.Errorf("Plan's shares: %d %v", status, body)
	}
	status, body = m.get(t, bobID, "/pano/"+doc+"/shares")
	expectStatus(t, "Bob listing the shares of Alice's doc", status, http.StatusNotFound, body)

	status, body = m.get(t, bobID, "/teams/"+teamTen+"/pano")
	docs, _ := body["docs"].([]any)
	if status != http.StatusOK || len(docs) != 1 {
		t.Fatalf("Bob's team Pano answered %d %v", status, body)
	}
	if d := docs[0].(map[string]any); d["id"] != doc || d["title"] != "Plan" || d["ownerName"] != "Alice" || d["ownerId"] != aliceID || d["markdown"] != nil {
		t.Errorf("the shared doc is listed as %v", d)
	}
	status, body = m.get(t, daveID, "/teams/"+teamTen+"/pano")
	expectStatus(t, "Dave, outside, listing the team's Pano", status, http.StatusNotFound, body)

	status, body = m.get(t, bobID, "/teams/"+teamTen+"/pano/"+doc)
	d, _ := body["doc"].(map[string]any)
	if status != http.StatusOK || d["markdown"] != "# Plan\n\n![chart](chart.png)" || d["ownerId"] != aliceID || d["ownerName"] != "Alice" || d["ownerPicture"] != "a.png" || d["tasks"] != nil || d["comments"] != nil {
		t.Fatalf("Bob reading Plan answered %d %v", status, body)
	}
	files, _ := d["files"].([]any)
	atts, _ := d["attachments"].([]any)
	if len(files) != 1 || len(atts) != 1 {
		t.Fatalf("Plan's files are %v and %v", files, atts)
	}
	png, _ := files[0].(map[string]any)["url"].(string)
	html, _ := atts[0].(map[string]any)["url"].(string)
	for _, u := range []string{png, html} {
		if !strings.HasPrefix(u, "/api/v1/teams/"+teamTen+"/pano/"+doc+"/files/") {
			t.Errorf("a file is linked by %q, not the team's route", u)
		}
	}
	status, hdr, raw := m.raw(t, bobID, strings.TrimPrefix(png, "/api/v1"))
	if status != http.StatusOK || raw != "png bytes" || hdr.Get("Content-Type") != "image/png" || !strings.HasPrefix(hdr.Get("Content-Disposition"), "inline") {
		t.Errorf("Bob reading the chart answered %d %v %q", status, hdr, raw)
	}
	status, hdr, _ = m.raw(t, bobID, strings.TrimPrefix(html, "/api/v1"))
	if status != http.StatusOK || hdr.Get("Content-Type") != "application/octet-stream" || !strings.HasSuffix(hdr.Get("Content-Security-Policy"), "; sandbox") {
		t.Errorf("an HTML file answered %d %v, want it served as one a browser cannot run", status, hdr)
	}

	for _, r := range []struct {
		what, user, path string
		want             int
	}{
		{"Dave, outside, reading Plan", daveID, "/teams/" + teamTen + "/pano/" + doc, http.StatusNotFound},
		{"Dave, outside, reading its chart", daveID, strings.TrimPrefix(png, "/api/v1"), http.StatusNotFound},
		{"Bob reading Alice's unshared doc", bobID, "/teams/" + teamTen + "/pano/" + mine, http.StatusNotFound},
		{"Bob reading a file by way of the unshared doc", bobID, "/teams/" + teamTen + "/pano/" + mine + "/files/" + strings.TrimPrefix(png, "/api/v1/teams/"+teamTen+"/pano/"+doc+"/files/"), http.StatusNotFound},
		{"a file of Plan that is not there", bobID, "/teams/" + teamTen + "/pano/" + doc + "/files/" + monoflake.ID(424242).String(), http.StatusNotFound},
		{"Bob reading an unshared doc's file by way of Plan", bobID, "/teams/" + teamTen + "/pano/" + doc + "/files/" + secretFile, http.StatusNotFound},
		{"Bob reading Dave's doc, shared with another team, by way of team 10", bobID, "/teams/" + teamTen + "/pano/" + daveDoc, http.StatusNotFound},
		{"Bob reading Dave's doc by way of Dave's team", bobID, "/teams/" + teamThirty + "/pano/" + daveDoc, http.StatusNotFound},
		{"Bob listing Dave's team's Pano", bobID, "/teams/" + teamThirty + "/pano", http.StatusNotFound},
		{"Dave reading Plan by way of his own team", daveID, "/teams/" + teamThirty + "/pano/" + doc, http.StatusNotFound},
		{"Alice reading Plan by way of Dave's team", aliceID, "/teams/" + teamThirty + "/pano/" + doc, http.StatusNotFound},
		{"Alice changing Dave's doc's shares", aliceID, "/pano/" + daveDoc + "/shares", http.StatusNotFound},
		{"a doc id that is not one", bobID, "/teams/" + teamTen + "/pano/!", http.StatusBadRequest},
		{"a file id that is not one", bobID, "/teams/" + teamTen + "/pano/" + doc + "/files/!", http.StatusBadRequest},
	} {
		if status, _, _ := m.raw(t, r.user, r.path); status != r.want {
			t.Errorf("%s answered %d, want %d", r.what, status, r.want)
		}
	}

	if docs := list(m.must(t, bobID, "/teams/"+teamTen+"/pano"), "docs", "id"); !sameList(docs, doc) {
		t.Errorf("Bob's team Pano lists %v, want only Plan: never Dave's, shared with another team", docs)
	}
	if docs := list(m.must(t, daveID, "/teams/"+teamThirty+"/pano"), "docs", "id"); !sameList(docs, daveDoc) {
		t.Errorf("Dave's team Pano lists %v, want only his own doc", docs)
	}

	v = m.teamVersion(t)
	status, body = call(t, m.app, aliceID, http.MethodDelete, "/pano/"+doc+"/shares/"+teamTen, "")
	expectStatus(t, "unsharing with no version", status, http.StatusBadRequest, body)
	status, body = call(t, m.app, aliceID, http.MethodDelete, fmt.Sprintf("/pano/%s/shares/%s?version=%d", doc, teamTen, v), "")
	if status != http.StatusOK || body["version"] != float64(v+1) {
		t.Errorf("Alice unsharing Plan answered %d %v", status, body)
	}
	if status, _, _ := m.raw(t, bobID, "/teams/"+teamTen+"/pano/"+doc); status != http.StatusNotFound {
		t.Errorf("Bob reading Plan after the unshare answered %d", status)
	}
	if status, _, _ := m.raw(t, bobID, strings.TrimPrefix(png, "/api/v1")); status != http.StatusNotFound {
		t.Errorf("Bob reading the chart after the unshare answered %d", status)
	}
	_, body = m.get(t, aliceID, "/teams/"+teamTen+"/activities")
	acts := list(body, "activities", "action")
	if len(acts) < 2 || acts[0] != "pano.unshared" || acts[1] != "pano.shared" {
		t.Errorf("the team's activity is %v, want the share and the unshare", acts)
	}
}
