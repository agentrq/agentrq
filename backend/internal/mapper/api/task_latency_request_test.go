// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
)

func TestFromHTTPRequestToGetTaskLatencyStatsRequestEntity(t *testing.T) {
	scoped := func(c *fiber.Ctx) *entity.GetTaskLatencyStatsRequest {
		return FromHTTPRequestToGetTaskLatencyStatsRequestEntity(c, true)
	}
	ws := "/workspaces/" + b62(20) + "/latency"
	checkRequest(t, "/workspaces/:id/latency", scoped, []requestCase[entity.GetTaskLatencyStatsRequest]{
		{"a week at the median by default", ws, ``, &entity.GetTaskLatencyStatsRequest{WorkspaceID: 20, Range: "7d", Aggregate: "p50"}},
		{"a custom range", ws + "?range=custom&from=100&to=200&aggregate=max", ``,
			&entity.GetTaskLatencyStatsRequest{WorkspaceID: 20, Range: "custom", From: 100, To: 200, Aggregate: "max"}},
		// Bounds that are not numbers are 0, for the controller to refuse.
		{"bounds that are not numbers", ws + "?range=custom&from=x&to=y&aggregate=min", ``,
			&entity.GetTaskLatencyStatsRequest{WorkspaceID: 20, Range: "custom", Aggregate: "min"}},
		{"an unknown aggregate", ws + "?aggregate=p99", ``, nil},
		{"workspace 0", "/workspaces/0/latency", ``, nil},
	})
	// Account-wide, the path names no workspace.
	checkRequest(t, "/latency", func(c *fiber.Ctx) *entity.GetTaskLatencyStatsRequest {
		return FromHTTPRequestToGetTaskLatencyStatsRequestEntity(c, false)
	}, []requestCase[entity.GetTaskLatencyStatsRequest]{
		{"the account's", "/latency?range=30d", ``, &entity.GetTaskLatencyStatsRequest{Range: "30d", Aggregate: "p50"}},
	})
}

func TestFromGetTaskLatencyStatsResponseEntityToHTTPResponse(t *testing.T) {
	s := func(v int64) *int64 { return &v }
	point := entity.TaskLatencyPoint{PeriodStart: 1000, Closed: 4,
		StartToClose: entity.TaskLatencyValue{Seconds: s(60), Count: 4}, Worked: entity.TaskLatencyValue{Seconds: s(40), Count: 4},
		Blocked: entity.TaskLatencyValue{Seconds: s(0), Count: 1}, NeedsInput: entity.TaskLatencyValue{}}
	wantPoint := view.TaskLatencyPoint{PeriodStart: 1000, Closed: 4,
		StartToClose: view.TaskLatencyValue{Seconds: s(60), Count: 4}, Worked: view.TaskLatencyValue{Seconds: s(40), Count: 4},
		Blocked: view.TaskLatencyValue{Seconds: s(0), Count: 1}, NeedsInput: view.TaskLatencyValue{}}

	raw := FromGetTaskLatencyStatsResponseEntityToHTTPResponse(&entity.GetTaskLatencyStatsResponse{
		Granularity: "day", Aggregate: "p50", RangeStart: 1000, RangeEnd: 2000,
		Points: []entity.TaskLatencyPoint{point, {PeriodStart: 1500}}, Summary: point,
	})
	want := view.TaskLatencyStats{Granularity: "day", Aggregate: "p50", RangeStart: 1000, RangeEnd: 2000,
		Points: []view.TaskLatencyPoint{wantPoint, {PeriodStart: 1500}},
		Summary: view.TaskLatencySummary{Closed: 4, StartToClose: wantPoint.StartToClose, Worked: wantPoint.Worked,
			Blocked: wantPoint.Blocked, NeedsInput: wantPoint.NeedsInput}}
	if got := decodeResponse[view.TaskLatencyStats](t, raw); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// A metric with no value is a gap, written as null, not 0.
	got := decodeResponse[map[string]any](t, raw)["summary"].(map[string]any)["needsInput"].(map[string]any)
	if v, ok := got["seconds"]; !ok || v != nil {
		t.Errorf("a gap is %v (present %v), want null", v, ok)
	}
}
