// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The suggested missions above a mission field, and the picker that writes
 * them: a pick pre-fills the text, and text the person wrote gets one undo.
 */

import { describe, it, expect } from 'vitest';
import { ref, nextTick } from 'vue';
import { MISSION_CATEGORIES, findMissionTemplate, isUntouchedMission } from '../src/utils/missionTemplates';
import { useMissionPicker } from '../src/composables/useMissionPicker';
import { DEFAULT_WORKSPACE_MISSION, WORKSPACE_WORKING_RULES } from '../src/utils/workspaceForm';

const category = (id) => MISSION_CATEGORIES.find((c) => c.id === id);
const sub = (catId, id) => category(catId).subcategories.find((s) => s.id === id);

describe('MISSION_CATEGORIES', () => {
  it('offers general, coding, manager, sales, marketing, research, legal, HR and ops, general first', () => {
    expect(MISSION_CATEGORIES.map((c) => c.label)).toEqual(['General', 'Coding', 'Manager', 'Sales', 'Marketing', 'Research', 'Legal', 'HR', 'Ops']);
  });

  it('breaks categories into the specialities asked for', () => {
    expect(category('coding').subcategories.map((s) => s.label)).toEqual(
      expect.arrayContaining(['Backend', 'Frontend', 'Full Stack', 'iOS', 'Android', 'Testing'])
    );
    expect(category('research').subcategories.map((s) => s.label)).toEqual(
      expect.arrayContaining(['Researcher', 'Finance Analyst'])
    );
    expect(category('manager').subcategories.map((s) => s.label)).toEqual(
      expect.arrayContaining(['PM', 'EM', 'TPM', 'Project', 'Release'])
    );
    expect(sub('manager', 'tpm').mission.split('\n')[0]).toBe(
      'This workspace is for technical program management (Manager).'
    );
    expect(category('hr').subcategories.map((s) => s.label)).toContain('Recruiter');
    expect(category('marketing').subcategories.map((s) => s.label)).toEqual(
      expect.arrayContaining(['Social Media', 'YouTube'])
    );
  });

  it('gives every speciality a distinct mission that names it and ends with the working rules', () => {
    const missions = MISSION_CATEGORIES.flatMap((c) => c.subcategories.map((s) => s.mission));
    expect(new Set(missions).size).toBe(missions.length);
    expect(sub('coding', 'ios').mission.split('\n')[0]).toBe('This workspace is for iOS app development (Coding).');
    for (const m of missions.filter((m) => m !== DEFAULT_WORKSPACE_MISSION)) {
      expect(m.startsWith('This workspace is for ')).toBe(true);
      expect(m).toContain('**Focus**\n- ');
      expect(m).toContain(WORKSPACE_WORKING_RULES);
    }
  });

  it('ends coding missions with the pull request checklist, and every other mission with the working rules', () => {
    for (const c of MISSION_CATEGORIES) {
      for (const s of c.subcategories.filter((s) => s.mission !== DEFAULT_WORKSPACE_MISSION)) {
        if (c.id === 'coding') {
          expect(s.mission).toContain(`${WORKSPACE_WORKING_RULES}\n\n**Pull requests**\n`);
          expect(s.mission.endsWith('- AgentRQ Task ID: <task ID>')).toBe(true);
          expect(s.mission).toContain('- Test coverage: target 100% unit test coverage on the new lines introduced');
        } else {
          expect(s.mission.endsWith(WORKSPACE_WORKING_RULES)).toBe(true);
        }
      }
    }
  });

  it('makes the form\'s default mission the General template', () => {
    expect(category('general').subcategories).toHaveLength(1);
    expect(sub('general', 'general').mission).toBe(DEFAULT_WORKSPACE_MISSION);
    expect(findMissionTemplate(DEFAULT_WORKSPACE_MISSION)).toEqual({ categoryId: 'general', subcategoryId: 'general' });
  });

  it('keeps legal templates from passing for legal advice', () => {
    for (const s of category('legal').subcategories) expect(s.mission).toContain('This is not legal advice');
  });

  it('keeps ids unique within each category', () => {
    for (const c of MISSION_CATEGORIES) {
      expect(new Set(c.subcategories.map((s) => s.id)).size).toBe(c.subcategories.length);
    }
  });
});

describe('isUntouchedMission', () => {
  it('holds for empty text, the default and an unedited template, surrounding space aside', () => {
    expect(isUntouchedMission('')).toBe(true);
    expect(isUntouchedMission(undefined)).toBe(true);
    expect(isUntouchedMission('  \n')).toBe(true);
    expect(isUntouchedMission(DEFAULT_WORKSPACE_MISSION)).toBe(true);
    expect(isUntouchedMission(`${sub('sales', 'crm').mission}\n`)).toBe(true);
  });

  it('fails for anything the person wrote', () => {
    expect(isUntouchedMission('Our billing service.')).toBe(false);
    expect(isUntouchedMission(`${sub('sales', 'crm').mission} Also HubSpot.`)).toBe(false);
  });
});

describe('findMissionTemplate', () => {
  it('names the category and speciality of an unedited template', () => {
    expect(findMissionTemplate(sub('marketing', 'youtube').mission)).toEqual({
      categoryId: 'marketing',
      subcategoryId: 'youtube',
    });
  });

  it('is null for other text', () => {
    expect(findMissionTemplate('mine')).toBeNull();
    expect(findMissionTemplate(null)).toBeNull();
  });
});

describe('useMissionPicker', () => {
  it('offers every category, and marks the template the mission already is', () => {
    const picker = useMissionPicker(ref(sub('coding', 'android').mission));
    expect(picker.categories).toBe(MISSION_CATEGORIES);
    expect(picker.selected.value).toEqual({ categoryId: 'coding', subcategoryId: 'android' });
    expect(picker.isSelected(category('coding'), sub('coding', 'android'))).toBe(true);
    expect(picker.isSelected(category('coding'), sub('coding', 'ios'))).toBe(false);
    expect(picker.holdsSelection(category('coding'))).toBe(true);
    expect(picker.holdsSelection(category('sales'))).toBe(false);
  });

  it('starts on the category row with General chosen for the default mission, and opens on another template', () => {
    const fresh = useMissionPicker(ref(DEFAULT_WORKSPACE_MISSION));
    expect(fresh.openCategory.value).toBeNull();
    expect(fresh.holdsSelection(category('general'))).toBe(true);
    expect(useMissionPicker(ref(sub('coding', 'android').mission)).openCategory.value.id).toBe('coding');
  });

  it('never opens a category with a single template, even on its mission', () => {
    const picker = useMissionPicker(ref(sub('general', 'general').mission));
    expect(picker.openCategory.value).toBeNull();
    expect(picker.holdsSelection(category('general'))).toBe(true);
  });

  it('opens a category to its specialities and goes back', () => {
    const mission = ref('');
    const picker = useMissionPicker(mission);
    picker.chooseCategory(category('research'));
    expect(picker.openCategory.value.label).toBe('Research');
    expect(mission.value).toBe('');
    picker.back();
    expect(picker.openCategory.value).toBeNull();
  });

  it('picks a single-template category at once, staying on the category row', async () => {
    const mission = ref('');
    const picker = useMissionPicker(mission);
    picker.chooseCategory(category('general'));
    await nextTick();
    expect(mission.value).toBe(sub('general', 'general').mission);
    expect(picker.openCategory.value).toBeNull();
  });

  it('opens on a template loaded after it started, but not over a category opened by hand', async () => {
    const mission = ref('');
    const picker = useMissionPicker(mission);
    mission.value = 'still loading';
    await nextTick();
    expect(picker.openCategory.value).toBeNull();

    mission.value = sub('ops', 'support').mission;
    await nextTick();
    expect(picker.openCategory.value.id).toBe('ops');

    picker.chooseCategory(category('sales'));
    mission.value = sub('coding', 'backend').mission;
    await nextTick();
    expect(picker.openCategory.value.id).toBe('sales');
  });

  it('replaces an untouched mission with no undo', async () => {
    const mission = ref(DEFAULT_WORKSPACE_MISSION);
    const picker = useMissionPicker(mission);
    picker.pick(sub('coding', 'backend'));
    await nextTick();
    expect(mission.value).toBe(sub('coding', 'backend').mission);
    expect(picker.undoText.value).toBeNull();

    picker.pick(sub('coding', 'testing'));
    await nextTick();
    expect(mission.value).toBe(sub('coding', 'testing').mission);
    expect(picker.undoText.value).toBeNull();
  });

  it('does nothing when the template picked is already the mission', async () => {
    const mission = ref('Mine.');
    const picker = useMissionPicker(mission);
    picker.pick(sub('sales', 'outreach'));
    await nextTick();
    picker.pick(sub('sales', 'outreach'));
    await nextTick();
    expect(picker.undoText.value).toBe('Mine.');
  });

  it('keeps written text for one undo, which puts it back', async () => {
    const mission = ref('Our billing service.');
    const picker = useMissionPicker(mission);
    picker.pick(sub('ops', 'devops'));
    await nextTick();
    expect(picker.undoText.value).toBe('Our billing service.');

    picker.undo();
    await nextTick();
    expect(mission.value).toBe('Our billing service.');
    expect(picker.undoText.value).toBeNull();

    picker.undo();
    expect(mission.value).toBe('Our billing service.');
  });

  it('drops the undo once the person edits the picked text', async () => {
    const mission = ref('Our billing service.');
    const picker = useMissionPicker(mission);
    picker.pick(sub('ops', 'admin'));
    await nextTick();
    mission.value += ' Plus payroll.';
    await nextTick();
    expect(picker.undoText.value).toBeNull();
  });
});
