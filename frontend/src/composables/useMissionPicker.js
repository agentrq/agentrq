// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref, computed, watch } from 'vue';
import { MISSION_CATEGORIES, findMissionTemplate, isUntouchedMission } from '../utils/missionTemplates';

/**
 * The state behind the template chips above a mission field: one row of
 * categories, which a category replaces with its specialities until the
 * person goes back. Picking a speciality writes its template into the field;
 * when that overwrites text the person wrote, the old text is kept for one
 * undo, since the app has no confirmation dialog to ask first.
 *
 * @param {import('vue').Ref<string>} mission the mission text, read and written
 */
// The category to open on for a mission: the one holding its template, unless
// that category is a single chip, which never opens.
function categoryToOpen(text) {
  const id = findMissionTemplate(text)?.categoryId;
  return MISSION_CATEGORIES.find((c) => c.id === id && c.subcategories.length > 1)?.id ?? null;
}

export function useMissionPicker(mission) {
  const openCategoryId = ref(categoryToOpen(mission.value));
  const undoText = ref(null);
  let applied = null;

  const openCategory = computed(() => MISSION_CATEGORIES.find((c) => c.id === openCategoryId.value) ?? null);
  const selected = computed(() => findMissionTemplate(mission.value));

  watch(mission, (text) => {
    // A settings form loads its mission after mount: open on the template it
    // holds, unless the person already opened a category.
    if (openCategoryId.value === null) openCategoryId.value = categoryToOpen(text);
    // Typing after a pick makes the new text theirs; the undo would lose it.
    if (text !== applied) undoText.value = null;
  });

  function isSelected(category, sub) {
    return selected.value?.categoryId === category.id && selected.value?.subcategoryId === sub.id;
  }

  /** Whether the category holds the mission's template, to mark it in the category row. */
  function holdsSelection(category) {
    return selected.value?.categoryId === category.id;
  }

  function pick(sub) {
    if (mission.value === sub.mission) return;
    const previous = mission.value;
    applied = sub.mission;
    mission.value = sub.mission;
    undoText.value = isUntouchedMission(previous) ? null : previous;
  }

  // A category with a single speciality has nothing to choose between, so it
  // is picked at once and the row stays as it is.
  function chooseCategory(category) {
    if (category.subcategories.length === 1) pick(category.subcategories[0]);
    else openCategoryId.value = category.id;
  }

  function back() {
    openCategoryId.value = null;
  }

  function undo() {
    if (undoText.value === null) return;
    applied = undoText.value;
    mission.value = undoText.value;
    undoText.value = null;
  }

  return {
    categories: MISSION_CATEGORIES,
    openCategory,
    selected,
    isSelected,
    holdsSelection,
    undoText,
    chooseCategory,
    back,
    pick,
    undo,
  };
}
