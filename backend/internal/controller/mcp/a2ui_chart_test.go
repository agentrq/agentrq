// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
)

// chartCard is a card in our catalog that draws a Chart.
var chartCard = `[
 {"version":"1.0","createSurface":{"surfaceId":"pr-1","catalogId":"` + a2ui.AgentRQCatalogID + `"}},
 {"version":"1.0","updateComponents":{"surfaceId":"pr-1","components":[
  {"id":"root","component":"Column","children":["title","chart"]},
  {"id":"title","component":"Text","text":"Merged this week"},
  {"id":"chart","component":"Chart","variant":"bar","label":"PRs","data":[{"label":"Mon","value":2},{"label":"Tue","value":5}]}
 ]}}
]`

func TestHandleReply_ACardWithAChartIsCounted(t *testing.T) {
	e := newA2UIReplyEnv(t)
	res, _, err := e.ps.handleReply(context.Background(), nil, ReplyParams{TaskID: "g", Text: "Merged this week", UI: json.RawMessage(chartCard)})
	if err != nil || res.IsError {
		t.Fatalf("reply = %v, %v", res, err)
	}
	if want := []entity.Action{entity.ActionA2UIReply, entity.ActionA2UIChart}; !reflect.DeepEqual(actions(e.events), want) {
		t.Errorf("events = %v, want %v", actions(e.events), want)
	}

	md, _ := json.Marshal(e.metadata)
	th := e.withThread(model.Message{ID: 555, TaskID: 42, Sender: "agent", Text: "Merged this week", Metadata: md})
	e.events = nil
	update := `[{"version":"1.0","updateComponents":{"surfaceId":"pr-1","components":[{"id":"chart","component":"Chart","variant":"line","data":[{"label":"Mon","value":3}]}]}}]`
	if res, _, _ := e.ps.handleReply(context.Background(), nil, updateParams("Merged", update)); res.IsError {
		t.Fatalf("update: %s", toolText(res))
	}
	if !strings.Contains(string(th.message(555).Metadata), `"variant":"line"`) {
		t.Errorf("the chart was not updated: %s", th.message(555).Metadata)
	}
	if want := []entity.Action{entity.ActionA2UIUpdate, entity.ActionA2UIChart}; !reflect.DeepEqual(actions(e.events), want) {
		t.Errorf("update events = %v, want %v", actions(e.events), want)
	}
}

func TestHandleReply_AChartNeedsOurCatalog(t *testing.T) {
	e := newA2UIReplyEnv(t)
	basic := strings.Replace(chartCard, a2ui.AgentRQCatalogID, a2ui.BasicCatalogID, 1)
	res, _, _ := e.ps.handleReply(context.Background(), nil, ReplyParams{TaskID: "g", Text: "x", UI: json.RawMessage(basic)})
	if !res.IsError || !strings.Contains(toolText(res), "Chart is in our catalog") {
		t.Errorf("reply = %s, want the Chart refused", toolText(res))
	}
}

func TestHandleElicit_AQuestionWithAChartIsCounted(t *testing.T) {
	e, _ := a2uiElicitEnv(t)
	answer(t, e.ps, "decline", nil)
	question := strings.Replace(strings.Replace(a2uiTestQuestion, a2ui.BasicCatalogID, a2ui.AgentRQCatalogID, 1),
		`"children":["q","go"]},`, `"children":["q","c","go"]},{"id":"c","component":"Chart","data":[{"label":"a","value":1}]},`, 1)
	if res, _, _ := e.ps.handleElicit(context.Background(), nil, a2uiElicitParams(question)); res.IsError {
		t.Fatalf("elicit: %s", toolText(res))
	}
	if want := []entity.Action{entity.ActionA2UIElicit, entity.ActionA2UIChart}; !reflect.DeepEqual(actions(e.events), want) {
		t.Errorf("events = %v, want %v", actions(e.events), want)
	}
}

func TestHandleReply_ACardWithAProgressIsCounted(t *testing.T) {
	e := newA2UIReplyEnv(t)
	card := strings.Replace(chartCard, `"children":["title","chart"]`, `"children":["title","chart","bar"]},{"id":"bar","component":"Progress","value":2,"max":5`, 1)
	if res, _, err := e.ps.handleReply(context.Background(), nil, ReplyParams{TaskID: "g", Text: "2 of 5", UI: json.RawMessage(card)}); err != nil || res.IsError {
		t.Fatalf("reply = %v, %v", res, err)
	}
	if want := []entity.Action{entity.ActionA2UIReply, entity.ActionA2UIChart, entity.ActionA2UIProgress}; !reflect.DeepEqual(actions(e.events), want) {
		t.Errorf("events = %v, want %v", actions(e.events), want)
	}
}
