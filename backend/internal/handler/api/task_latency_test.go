// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

type latencyCrud struct {
	crud.Controller
	err error
	saw entity.GetTaskLatencyStatsRequest
}

func (l *latencyCrud) GetTaskLatencyStats(_ context.Context, req entity.GetTaskLatencyStatsRequest) (*entity.GetTaskLatencyStatsResponse, error) {
	l.saw = req
	if l.err != nil {
		return nil, l.err
	}
	v := int64(90)
	return &entity.GetTaskLatencyStatsResponse{
		Granularity: "day", Aggregate: req.Aggregate, RangeStart: 1, RangeEnd: 2,
		Points:  []entity.TaskLatencyPoint{{PeriodStart: 0, Closed: 1, Worked: entity.TaskLatencyValue{Seconds: &v, Count: 1}}},
		Summary: entity.TaskLatencyPoint{Closed: 1, Worked: entity.TaskLatencyValue{Seconds: &v, Count: 1}},
	}, nil
}

func latencyApp(c crud.Controller) *fiber.App {
	h := &handler{crud: c}
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Locals("user_id", "user-1")
		return ctx.Next()
	})
	app.Get("/stats/latency", h.getTaskLatencyStats(false))
	app.Get("/workspaces/:id/stats/latency", h.getTaskLatencyStats(true))
	return app
}

func TestGetTaskLatencyStats_Workspace(t *testing.T) {
	c := &latencyCrud{}
	ws := monoflake.ID(4242).String()
	res, _ := latencyApp(c).Test(httptest.NewRequest(http.MethodGet, "/workspaces/"+ws+"/stats/latency?range=custom&from=10&to=20&aggregate=max", nil))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if c.saw != (entity.GetTaskLatencyStatsRequest{WorkspaceID: 4242, UserID: "user-1", Range: "custom", From: 10, To: 20, Aggregate: "max"}) {
		t.Fatalf("request = %+v", c.saw)
	}
	var body map[string]any
	raw, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"granularity", "aggregate", "rangeStart", "rangeEnd", "points", "summary"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing %q in %s", key, raw)
		}
	}
	point := body["points"].([]any)[0].(map[string]any)
	if point["worked"].(map[string]any)["seconds"] != float64(90) || point["startToClose"].(map[string]any)["seconds"] != nil {
		t.Errorf("point = %v", point)
	}
	if _, ok := point["needsInput"]; !ok {
		t.Errorf("point = %v", point)
	}
}

func TestGetTaskLatencyStats_AccountDefaults(t *testing.T) {
	c := &latencyCrud{}
	res, _ := latencyApp(c).Test(httptest.NewRequest(http.MethodGet, "/stats/latency", nil))
	if res.StatusCode != http.StatusOK || c.saw.WorkspaceID != 0 || c.saw.Range != "7d" || c.saw.Aggregate != "p50" {
		t.Fatalf("status %d, request %+v", res.StatusCode, c.saw)
	}
}

func TestGetTaskLatencyStats_Refused(t *testing.T) {
	for _, path := range []string{
		"/stats/latency?aggregate=avg",
		"/stats/latency?aggregate=p99",
		"/workspaces/!/stats/latency",
	} {
		res, _ := latencyApp(&latencyCrud{}).Test(httptest.NewRequest(http.MethodGet, path, nil))
		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", path, res.StatusCode)
		}
	}
	res, _ := latencyApp(&latencyCrud{err: base.ErrNotFound}).Test(httptest.NewRequest(http.MethodGet, "/workspaces/"+monoflake.ID(7).String()+"/stats/latency", nil))
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("someone else's workspace: status = %d, want 404", res.StatusCode)
	}
}
