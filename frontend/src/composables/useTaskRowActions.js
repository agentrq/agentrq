// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The buttons a task row offers, for every list that draws one.
 *
 * A workspace's own list and the account-wide lists behind the sidebar links
 * show the same rows, and they drifted: the sidebar's had no edit or delete at
 * all. Deciding it here keeps them one rule.
 */

/** Only a task that has not run yet, or a schedule, can still be edited. */
export function canEditTask(task, archived) {
  return !archived && (task?.status === 'cron' || task?.status === 'notstarted');
}

/** An archived workspace is read-only; everything else can be deleted. */
export function canDeleteTask(task, archived) {
  return !archived && !!task;
}

/** The edit form lives under the task's own workspace, whichever list opened it. */
export function taskEditPath(task) {
  return `/workspaces/${task.workspaceId}/tasks/${task.id}/edit`;
}

/** The list without the task, compared as strings since ids arrive both ways. */
export function withoutTask(tasks, taskId) {
  return tasks.filter((t) => String(t.id) !== String(taskId));
}
