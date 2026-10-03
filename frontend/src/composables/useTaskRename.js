// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { computed, ref } from 'vue';
import { updateTaskTitle } from '../api';

/**
 * How long after it was created a task can be renamed. The server enforces the
 * same window (entity.TaskTitleEditWindow); this only decides whether to offer
 * a click on the title at all.
 */
export const TASK_RENAME_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

export function canRenameTask(task, now = Date.now()) {
  const created = Date.parse(task?.createdAt ?? '');
  return Number.isFinite(created) && now - created <= TASK_RENAME_WINDOW_MS;
}

/**
 * Renaming the task a page shows, in place.
 *
 * Enter and blur both save, and an Enter is followed by the blur of the input
 * it removes, so a save already under way is not started again. Saving the
 * title it already has, or nothing, just closes the editor.
 *
 * @param {import('vue').Ref} task - the page's task; replaced by the server's copy on save
 * @param {import('vue').Ref<string>} workspaceId
 * @param {{ onError: (message: string) => void, now?: () => number }} options
 */
export function useTaskRename(task, workspaceId, { onError, now = Date.now }) {
  const editing = ref(false);
  const draft = ref('');
  const saving = ref(false);

  const renamable = computed(() => !!task.value && canRenameTask(task.value, now()));

  const start = () => {
    if (!renamable.value) return;
    draft.value = task.value.title;
    editing.value = true;
  };

  const cancel = () => {
    editing.value = false;
  };

  const save = async () => {
    if (!editing.value || saving.value) return;
    const title = draft.value.trim();
    if (!title || title === task.value.title) {
      editing.value = false;
      return;
    }
    saving.value = true;
    try {
      const res = await updateTaskTitle(workspaceId.value, task.value.id, title);
      task.value = res.task;
      editing.value = false;
    } catch (err) {
      onError('Could not rename the task: ' + err.message);
    } finally {
      saving.value = false;
    }
  };

  return { editing, draft, saving, renamable, start, cancel, save };
}
