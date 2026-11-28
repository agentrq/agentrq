// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { teamIdOf, withTeam } from './useTeamMode';

/**
 * Where the task page's back arrow goes: the list the task was opened from,
 * never the browser history, which may lead out of the app or to another task.
 *
 * @param {{ path: string, params: Record<string, string>, query?: object }} route
 */
export function taskListPath(route) {
  const team = teamIdOf(route);
  return team ? withTeam(listPath(route), team) : listPath(route);
}

function listPath(route) {
  const { params } = route;
  if (params.filter) return { path: `/tasks/${params.filter}`, query: route.query };
  if (params.docId) return { path: `/pano/${params.docId}` };
  if (route.path.startsWith('/pano/')) return { path: '/pano' };
  if (/(^|\/)kanban\/chat\//.test(route.path)) return { path: '/kanban', query: route.query };
  if (route.path.includes('/board/chat/')) return { path: `/workspaces/${params.id}/board` };
  return { path: `/workspaces/${params.id}` };
}

/**
 * The task's own page, for a task opened beside a board or a doc (a `chat`
 * route); null when the task already has the page to itself.
 *
 * @param {{ path: string, params: Record<string, string> }} route
 */
export function taskPagePath(route) {
  if (!/\/chat\//.test(route.path)) return null;
  const { params } = route;
  const path = `/workspaces/${params.workspaceId || params.id}/tasks/${params.taskId}`;
  const team = teamIdOf(route);
  return team ? withTeam(path, team) : { path };
}
