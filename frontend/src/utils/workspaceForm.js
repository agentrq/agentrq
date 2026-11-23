// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The self-learning loop note a new workspace starts with. The server applies
 * the same text to a workspace created with none, so a workspace made over MCP
 * gets it too; it is repeated here only so the create form can show it, and a
 * test fails when the two copies differ.
 */
export const DEFAULT_SELF_LEARNING_LOOP_NOTE = `Just before marking the task as completed:

**Self-learning loop**
- Look back over the task for what a future task would be better off knowing, and save it to this workspace's memory or skills:
  - A correction from the human (style, format, workflow, "stop doing X"): record the preference, so the next task starts already knowing it.
  - Something you struggled with and solved, or a more efficient way you found to do it.
  - A memory or skill you relied on that turned out wrong, incomplete or outdated: fix it now.
- Update the memory or skill that already covers the topic rather than adding a near-duplicate, and rewrite any older note the new lesson contradicts.
- Write the rule, not the story: a line or two, linked from the memory index. If nothing was learned, save nothing.`;

/**
 * The working rules every suggested mission ends with, the default one and
 * each category's template alike.
 */
export const WORKSPACE_WORKING_RULES = `**Working rules**
- Keep the human up to date at every milestone: they only see what you send through AgentRQ.
- When you are blocked, or a decision is the human's to make, ask rather than guess.
- Before marking a task completed, summarise what you changed and why.`;

/**
 * The mission a new workspace's form starts with: the working rules most
 * workspaces want anyway, which is also the General template. Only the form
 * offers it; a workspace created elsewhere without one keeps none.
 */
export const DEFAULT_WORKSPACE_MISSION = WORKSPACE_WORKING_RULES;

/** A blank create-workspace form, with the default mission and note filled in to edit. */
export function emptyWorkspaceForm() {
  return {
    name: '',
    description: DEFAULT_WORKSPACE_MISSION,
    icon: '',
    selfLearningLoopNote: DEFAULT_SELF_LEARNING_LOOP_NOTE,
    workingDirectory: '',
  };
}

/** Whether the create form can be sent: a workspace needs a name, and nothing else. */
export function canCreateWorkspace(form) {
  return Boolean(form?.name?.trim());
}
