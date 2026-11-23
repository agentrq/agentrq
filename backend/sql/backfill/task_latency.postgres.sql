-- Copyright 2026 Contextual, Inc. https://agentrq.com
-- This notice may not be modified or removed.
-- SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

-- Backfill task latency (postgres): one task_latencies row per task whose
-- history ends closed (completed/rejected), then the hourly, daily and monthly
-- rollups over every complete hour/day, and the claims that tell the stats API
-- the rollups are there. Safe to re-run: task rows written by the server are
-- kept; rollup rows are recomputed from task_latencies.
--
-- The server runs the same backfill by itself, once, on the first boot of the
-- version that records latency (latencyaggregator, claim latency_backfill/v1).
-- This file is the fallback if that run failed, as logged at boot.

-- 1. One row per closed task, from its whole history — the arithmetic of
--    TaskTimingOf: each state lasts until the next transition.
WITH t AS (
  SELECT id, task_id, user_id, workspace_id, to_state, created_at,
         LEAD(created_at) OVER (PARTITION BY task_id ORDER BY created_at, id) AS next_at,
         ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at DESC, id DESC) AS rn
  FROM task_state_transitions
),
closed AS (
  SELECT task_id, user_id, workspace_id, created_at AS closed_ts FROM t WHERE rn = 1 AND to_state IN (4, 5)
),
started AS (
  SELECT task_id, MIN(created_at) AS started_ts FROM t WHERE to_state = 2 GROUP BY task_id
),
spans AS (
  SELECT task_id, to_state, FLOOR(EXTRACT(EPOCH FROM (next_at - created_at)))::bigint AS secs FROM t WHERE next_at IS NOT NULL
)
INSERT INTO task_latencies (task_id, user_id, workspace_id, closed_at, start_to_close_seconds, worked_seconds, blocked_seconds, needs_input_seconds)
SELECT c.task_id, c.user_id, c.workspace_id, FLOOR(EXTRACT(EPOCH FROM c.closed_ts))::bigint,
       CASE WHEN st.started_ts IS NULL THEN NULL ELSE FLOOR(EXTRACT(EPOCH FROM (c.closed_ts - st.started_ts)))::bigint END,
       COALESCE(SUM(CASE WHEN sp.to_state = 2 THEN sp.secs END), 0),
       COALESCE(SUM(CASE WHEN sp.to_state = 3 THEN sp.secs END), 0),
       COALESCE(SUM(CASE WHEN sp.to_state = 7 THEN sp.secs END), 0)
FROM closed c
LEFT JOIN started st ON st.task_id = c.task_id
LEFT JOIN spans sp ON sp.task_id = c.task_id
WHERE 1 = 1
GROUP BY c.task_id, c.user_id, c.workspace_id, c.closed_ts, st.started_ts
ON CONFLICT (task_id) DO NOTHING;

-- 2. hourly_task_latencies: every complete hour. Negative spans (clock skew) count as 0.
WITH v AS (
  SELECT user_id, workspace_id, closed_at, 1 AS metric, start_to_close_seconds AS val FROM task_latencies WHERE start_to_close_seconds IS NOT NULL
  UNION ALL SELECT user_id, workspace_id, closed_at, 2, worked_seconds FROM task_latencies
  UNION ALL SELECT user_id, workspace_id, closed_at, 3, blocked_seconds FROM task_latencies
  UNION ALL SELECT user_id, workspace_id, closed_at, 4, needs_input_seconds FROM task_latencies
),
m AS (
  SELECT user_id, workspace_id, metric, (closed_at / 3600) * 3600 AS period_start, CASE WHEN val < 0 THEN 0 ELSE val END AS s
  FROM v WHERE closed_at < ((FLOOR(EXTRACT(EPOCH FROM now()))::bigint) / 3600) * 3600
)
INSERT INTO hourly_task_latencies (period_start, user_id, workspace_id, metric, count, sum, min, max, histogram)
SELECT period_start, user_id, workspace_id, metric, COUNT(*), SUM(s), MIN(s), MAX(s),
    CAST(SUM(CASE WHEN s < 60 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 60 AND s < 120 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 120 AND s < 300 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 300 AND s < 600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 600 AND s < 900 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 900 AND s < 1800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 1800 AND s < 3600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 3600 AND s < 7200 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 7200 AND s < 14400 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 14400 AND s < 28800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 28800 AND s < 43200 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 43200 AND s < 86400 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 86400 AND s < 172800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 172800 AND s < 345600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 345600 AND s < 604800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 604800 AND s < 1209600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 1209600 AND s < 2592000 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 2592000 THEN 1 ELSE 0 END) AS TEXT)
FROM m
WHERE 1 = 1
GROUP BY period_start, user_id, workspace_id, metric
ON CONFLICT (period_start, user_id, workspace_id, metric) DO UPDATE SET
  count = excluded.count, sum = excluded.sum, min = excluded.min, max = excluded.max, histogram = excluded.histogram;

-- 3. daily_task_latencies: every complete day. Negative spans (clock skew) count as 0.
WITH v AS (
  SELECT user_id, workspace_id, closed_at, 1 AS metric, start_to_close_seconds AS val FROM task_latencies WHERE start_to_close_seconds IS NOT NULL
  UNION ALL SELECT user_id, workspace_id, closed_at, 2, worked_seconds FROM task_latencies
  UNION ALL SELECT user_id, workspace_id, closed_at, 3, blocked_seconds FROM task_latencies
  UNION ALL SELECT user_id, workspace_id, closed_at, 4, needs_input_seconds FROM task_latencies
),
m AS (
  SELECT user_id, workspace_id, metric, (closed_at / 86400) * 86400 AS period_start, CASE WHEN val < 0 THEN 0 ELSE val END AS s
  FROM v WHERE closed_at < ((FLOOR(EXTRACT(EPOCH FROM now()))::bigint) / 86400) * 86400
)
INSERT INTO daily_task_latencies (period_start, user_id, workspace_id, metric, count, sum, min, max, histogram)
SELECT period_start, user_id, workspace_id, metric, COUNT(*), SUM(s), MIN(s), MAX(s),
    CAST(SUM(CASE WHEN s < 60 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 60 AND s < 120 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 120 AND s < 300 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 300 AND s < 600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 600 AND s < 900 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 900 AND s < 1800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 1800 AND s < 3600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 3600 AND s < 7200 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 7200 AND s < 14400 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 14400 AND s < 28800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 28800 AND s < 43200 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 43200 AND s < 86400 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 86400 AND s < 172800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 172800 AND s < 345600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 345600 AND s < 604800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 604800 AND s < 1209600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 1209600 AND s < 2592000 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 2592000 THEN 1 ELSE 0 END) AS TEXT)
FROM m
WHERE 1 = 1
GROUP BY period_start, user_id, workspace_id, metric
ON CONFLICT (period_start, user_id, workspace_id, metric) DO UPDATE SET
  count = excluded.count, sum = excluded.sum, min = excluded.min, max = excluded.max, histogram = excluded.histogram;

-- 4. monthly_task_latencies: every month, the current one through yesterday. Negative spans (clock skew) count as 0.
WITH v AS (
  SELECT user_id, workspace_id, closed_at, 1 AS metric, start_to_close_seconds AS val FROM task_latencies WHERE start_to_close_seconds IS NOT NULL
  UNION ALL SELECT user_id, workspace_id, closed_at, 2, worked_seconds FROM task_latencies
  UNION ALL SELECT user_id, workspace_id, closed_at, 3, blocked_seconds FROM task_latencies
  UNION ALL SELECT user_id, workspace_id, closed_at, 4, needs_input_seconds FROM task_latencies
),
m AS (
  SELECT user_id, workspace_id, metric, CAST(EXTRACT(EPOCH FROM date_trunc('month', to_timestamp(closed_at) AT TIME ZONE 'UTC')) AS bigint) AS period_start, CASE WHEN val < 0 THEN 0 ELSE val END AS s
  FROM v WHERE closed_at < ((FLOOR(EXTRACT(EPOCH FROM now()))::bigint) / 86400) * 86400
)
INSERT INTO monthly_task_latencies (period_start, user_id, workspace_id, metric, count, sum, min, max, histogram)
SELECT period_start, user_id, workspace_id, metric, COUNT(*), SUM(s), MIN(s), MAX(s),
    CAST(SUM(CASE WHEN s < 60 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 60 AND s < 120 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 120 AND s < 300 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 300 AND s < 600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 600 AND s < 900 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 900 AND s < 1800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 1800 AND s < 3600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 3600 AND s < 7200 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 7200 AND s < 14400 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 14400 AND s < 28800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 28800 AND s < 43200 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 43200 AND s < 86400 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 86400 AND s < 172800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 172800 AND s < 345600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 345600 AND s < 604800 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 604800 AND s < 1209600 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 1209600 AND s < 2592000 THEN 1 ELSE 0 END) AS TEXT)
    || ',' || CAST(SUM(CASE WHEN s >= 2592000 THEN 1 ELSE 0 END) AS TEXT)
FROM m
WHERE 1 = 1
GROUP BY period_start, user_id, workspace_id, metric
ON CONFLICT (period_start, user_id, workspace_id, metric) DO UPDATE SET
  count = excluded.count, sum = excluded.sum, min = excluded.min, max = excluded.max, histogram = excluded.histogram;

-- 5. The claims: the stats API reads rollups up to the newest claim of each
--    type and the task rows after it.
INSERT INTO telemetry_aggregations (created_at, aggregation_type, period_key) VALUES
  (now(), 'latency_hourly', to_char(to_timestamp(((FLOOR(EXTRACT(EPOCH FROM now()))::bigint) / 3600) * 3600 - 3600) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24')),
  (now(), 'latency_daily', to_char(to_timestamp(((FLOOR(EXTRACT(EPOCH FROM now()))::bigint) / 86400) * 86400 - 86400) AT TIME ZONE 'UTC', 'YYYY-MM-DD')),
  (now(), 'latency_monthly', to_char(to_timestamp(((FLOOR(EXTRACT(EPOCH FROM now()))::bigint) / 86400) * 86400) AT TIME ZONE 'UTC', 'YYYY-MM-DD'))
ON CONFLICT DO NOTHING;
