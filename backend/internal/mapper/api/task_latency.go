// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strconv"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
)

// FromHTTPRequestToGetTaskLatencyStatsRequestEntity reads
// ?range=&from=&to=&aggregate= and, when workspaceScoped, the workspace from
// the path. aggregate defaults to p50; any other value than p50, min or max
// is refused, as is a missing workspace.
func FromHTTPRequestToGetTaskLatencyStatsRequestEntity(c fiber.Ctx, workspaceScoped bool) *entity.GetTaskLatencyStatsRequest {
	rq := &entity.GetTaskLatencyStatsRequest{
		Range:     c.Query("range", "7d"),
		Aggregate: c.Query("aggregate", tasklatency.AggregateP50),
	}
	if !tasklatency.ValidAggregate(rq.Aggregate) {
		return nil
	}
	if workspaceScoped {
		rq.WorkspaceID = monoflake.IDFromBase62(c.Params("id")).Int64()
		if rq.WorkspaceID == 0 {
			return nil
		}
	}
	rq.From, _ = strconv.ParseInt(c.Query("from"), 10, 64)
	rq.To, _ = strconv.ParseInt(c.Query("to"), 10, 64)
	return rq
}

func fromTaskLatencyValueEntityToView(v entity.TaskLatencyValue) view.TaskLatencyValue {
	return view.TaskLatencyValue{Seconds: v.Seconds, Count: v.Count}
}

func fromTaskLatencyPointEntityToView(p entity.TaskLatencyPoint) view.TaskLatencyPoint {
	return view.TaskLatencyPoint{
		PeriodStart:  p.PeriodStart,
		Closed:       p.Closed,
		StartToClose: fromTaskLatencyValueEntityToView(p.StartToClose),
		Worked:       fromTaskLatencyValueEntityToView(p.Worked),
		Blocked:      fromTaskLatencyValueEntityToView(p.Blocked),
		NeedsInput:   fromTaskLatencyValueEntityToView(p.NeedsInput),
	}
}

// FromGetTaskLatencyStatsResponseEntityToHTTPResponse renders the latency
// statistics.
func FromGetTaskLatencyStatsResponseEntityToHTTPResponse(rs *entity.GetTaskLatencyStatsResponse) []byte {
	if rs == nil {
		return nil
	}
	out := view.TaskLatencyStats{
		Granularity: rs.Granularity,
		Aggregate:   rs.Aggregate,
		RangeStart:  rs.RangeStart,
		RangeEnd:    rs.RangeEnd,
		Points:      make([]view.TaskLatencyPoint, len(rs.Points)),
		Summary: view.TaskLatencySummary{
			Closed:       rs.Summary.Closed,
			StartToClose: fromTaskLatencyValueEntityToView(rs.Summary.StartToClose),
			Worked:       fromTaskLatencyValueEntityToView(rs.Summary.Worked),
			Blocked:      fromTaskLatencyValueEntityToView(rs.Summary.Blocked),
			NeedsInput:   fromTaskLatencyValueEntityToView(rs.Summary.NeedsInput),
		},
	}
	for i, p := range rs.Points {
		out.Points[i] = fromTaskLatencyPointEntityToView(p)
	}
	data, _ := json.Marshal(out)
	return data
}
