// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The default self-learning loop note, and the create form it fills.
 *
 * The server gives the same note to a workspace created without one, so the
 * form's copy must match it: otherwise a workspace made here and one made over
 * MCP would start with different instructions. The server's copy is read from
 * its source rather than kept in a list by hand.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  DEFAULT_SELF_LEARNING_LOOP_NOTE,
  DEFAULT_WORKSPACE_MISSION,
  canCreateWorkspace,
  emptyWorkspaceForm,
} from '../src/utils/workspaceForm';

function serverNote() {
  const src = readFileSync(resolve(__dirname, '../../backend/internal/controller/crud/workspace.go'), 'utf-8');
  const match = src.match(/const defaultWorkspaceSelfLearningLoopNote = `([^`]*)`/);
  if (!match) throw new Error('defaultWorkspaceSelfLearningLoopNote not found in the server source');
  return match[1];
}

describe('the default self-learning loop note', () => {
  it('is the note the server gives a workspace created without one', () => {
    expect(DEFAULT_SELF_LEARNING_LOOP_NOTE).toBe(serverNote());
  });
});

describe('emptyWorkspaceForm', () => {
  it('is blank but for the default mission and note', () => {
    expect(emptyWorkspaceForm()).toEqual({
      name: '',
      description: DEFAULT_WORKSPACE_MISSION,
      icon: '',
      selfLearningLoopNote: DEFAULT_SELF_LEARNING_LOOP_NOTE,
      workingDirectory: '',
    });
  });

  it('is a fresh object each time, so an edit does not leak into the next form', () => {
    const first = emptyWorkspaceForm();
    first.description = 'edited';
    first.selfLearningLoopNote = 'edited';
    expect(emptyWorkspaceForm().description).toBe(DEFAULT_WORKSPACE_MISSION);
    expect(emptyWorkspaceForm().selfLearningLoopNote).toBe(DEFAULT_SELF_LEARNING_LOOP_NOTE);
  });
});

describe('canCreateWorkspace', () => {
  it('needs a name that is more than whitespace', () => {
    expect(canCreateWorkspace({ name: 'billing' })).toBe(true);
    expect(canCreateWorkspace({ name: '   ' })).toBe(false);
    expect(canCreateWorkspace(emptyWorkspaceForm())).toBe(false);
  });

  it('is false for no form at all', () => {
    expect(canCreateWorkspace(undefined)).toBe(false);
  });
});
