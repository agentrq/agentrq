// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Where the task page's back arrow goes: the list the task was opened from,
 * never the browser history, which may lead out of the app or to another task.
 *
 * @param {{ path: string, params: Record<string, string>, query?: object }} route
 */
export function taskListPath(route) {
  const { params } = route;
  if (params.filter) return { path: `/tasks/${params.filter}`, query: route.query };
  if (params.docId) return { path: `/pano/${params.docId}` };
  if (route.path.startsWith('/pano/')) return { path: '/pano' };
  return { path: `/workspaces/${params.id}` };
}
