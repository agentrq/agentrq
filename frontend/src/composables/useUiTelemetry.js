// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * Counting how the interface itself gets used.
 *
 * The backend emits an event for every piece of work it does, which makes those
 * counts self-evidently true. Pressing a shortcut, searching, copying a link or
 * switching to the trajectory begins and ends in the tab — the server sees
 * nothing at all — so, exactly like the local-AI metrics, the browser has to say
 * so itself.
 *
 * ## Why a workspace has to be resolved first
 *
 * `recordTelemetry` takes a workspace, and the backend checks the caller owns
 * it. That check is the whole security boundary for the ingest route: without
 * it a caller could attribute its own clicks to somebody else's workspace and
 * corrupt a metric for a user who never generated it.
 *
 * Some of these actions happen where there is no workspace — Cmd+K from the
 * task list, a shortcut on the workspace list. Those reports are **dropped**.
 * Attributing them to a guessed workspace would be inventing data, which is
 * worse than counting less of it, and there is no honest guess available: the
 * route is the only thing that knows.
 *
 * The practical consequence is worth stating where someone reads the numbers:
 * these are counts of *actions taken with a workspace in context*, not of every
 * action. Because `ui_search` and `ui_search_open` resolve the workspace the
 * same way at the same moment, the ratio between them stays meaningful even
 * though the absolute figures are a floor.
 */
import { recordTelemetry } from '../api';

/**
 * The workspace a route is about, or '' when it is not about one.
 *
 * Two shapes, because the route table spells the same thing differently: the
 * workspace pages carry it as `id`, while a task opened from a filtered list
 * carries it as `workspaceId`. Missing one would silently drop every report
 * from half the app.
 *
 * @param {{ params?: Record<string, unknown> }} route
 * @returns {string}
 */
export function workspaceIdFromRoute(route) {
  const params = route?.params ?? {};
  const candidate = params.workspaceId ?? (isWorkspaceRoute(route) ? params.id : '');
  return typeof candidate === 'string' ? candidate : '';
}

/**
 * `:id` means a workspace only under `/workspaces`. Elsewhere — `/events/:id`,
 * `/workflows/:id` — it is a different thing entirely, and reporting it as a
 * workspace would be rejected by the backend at best and misattributed at
 * worst.
 */
function isWorkspaceRoute(route) {
  return typeof route?.path === 'string' && route.path.startsWith('/workspaces/');
}

/**
 * Report one interface action, if it can be attributed.
 *
 * Never throws and never returns anything worth checking: a metric is not worth
 * failing a click over, which is also why `recordTelemetry` itself swallows
 * transport failures.
 *
 * @param {string} action one of the `TELEMETRY_UI_*` names
 * @param {{ params?: object, path?: string }} route the current route
 * @param {(action: string, workspaceId: string) => void} [record] test seam
 */
export function recordUiAction(action, route, record = recordTelemetry) {
  const workspaceId = workspaceIdFromRoute(route);
  // `recordTelemetry` already ignores an empty workspace; checking here as well
  // keeps that a decision this module makes rather than a behaviour it relies
  // on another module continuing to have.
  if (!workspaceId) return;
  record(action, workspaceId);
}

/**
 * A "once per episode" gate.
 *
 * The finder searches on every keystroke, so reporting each one would count
 * typing "deploy" as six searches and make the number meaningless. This records
 * the first search of a session and nothing more until the session restarts.
 *
 * @returns {{ fire: (fn: () => void) => void, reset: () => void }}
 */
export function createOncePerEpisode() {
  let fired = false;
  return {
    fire(fn) {
      if (fired) return;
      fired = true;
      fn();
    },
    reset() {
      fired = false;
    },
  };
}
