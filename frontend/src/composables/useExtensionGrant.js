import { computed, ref } from 'vue';

/**
 * What the install screen asks, and what the answer means.
 *
 * Two conversations happen here and only one of them scales.
 *
 * **The author conversation is constant.** Every extension runs with full access
 * to the computer — there is no sandbox, by design — so that sentence appears on
 * every install, and the screen leads with the repository, its owner, its stars
 * and its licence. The decision being made is whether to trust a person.
 *
 * **The permission conversation scales with the ask.** An extension that wants
 * nothing from AgentRQ shows no permission list at all. One that wants workspace
 * tools shows them, and the workspace they will apply to. One that wants the
 * supervisor shows that it reaches the whole account.
 *
 * ## The emptiest screen must not describe the least-restrained code
 *
 * An extension declaring no `mcp` still shows the install confirmation. It is
 * the case where a permission list would be *absent*, and absence reads as
 * safety — so the sentence about full machine access carries the whole weight
 * there and must not be dropped along with the list.
 */

/** The rungs, in the order they escalate. */
export const SCOPE = {
  workspace: 'workspace',
  selected: 'selected',
  supervisor: 'supervisor',
};

export const SCOPE_ORDER = [SCOPE.workspace, SCOPE.selected, SCOPE.supervisor];

/**
 * What a manifest is asking for.
 *
 * `level` decides how much of the screen appears; the two tool lists are shown
 * verbatim, because a summary of what an extension may do is a paraphrase of a
 * permission and this is not the place to paraphrase.
 */
export function describeAsk(manifest) {
  const workspace = manifest?.mcp?.workspace ?? [];
  const supervisor = manifest?.mcp?.supervisor ?? [];

  return {
    workspace,
    supervisor,
    level: supervisor.length > 0 ? 'supervisor' : workspace.length > 0 ? 'workspace' : 'none',
    // The one thing that is always true, whatever the level.
    machineAccess: 'This extension runs with full access to your computer.',
  };
}

/**
 * Which rungs an extension can be given.
 *
 * An extension asking for no supervisor tools is not offered the supervisor
 * rung: offering a grant nothing would use invites somebody to hand over the
 * account for no reason at all.
 */
export function availableScopes(ask) {
  if (ask.level === 'supervisor') return [...SCOPE_ORDER];
  if (ask.level === 'workspace') return [SCOPE.workspace, SCOPE.selected];
  return [];
}

/**
 * Whether a chosen grant is coherent and complete.
 *
 * The supervisor rung deliberately has no workspace list. `listAllTasks` spans
 * the platform and `createTask(workspaceId)` reaches anywhere, so "supervisor,
 * but only this workspace" is not a narrower grant — it is the same grant with a
 * misleading label. The ladder makes it unrepresentable rather than merely
 * discouraged.
 */
export function validateGrant(ask, choice) {
  const scope = choice?.scope;
  if (ask.level === 'none') return { ok: true };

  // Checked before the general case, because it has a far better answer.
  // "Choose what this extension may reach" is true but unhelpful when the
  // problem is that the extension never asked for what was picked.
  if (scope === SCOPE.supervisor && ask.level !== 'supervisor') {
    return { ok: false, reason: 'This extension did not ask for access to all workspaces.' };
  }

  if (!availableScopes(ask).includes(scope)) {
    return { ok: false, reason: 'Choose what this extension may reach.' };
  }

  if (scope === SCOPE.selected && (choice.workspaces ?? []).length === 0) {
    return { ok: false, reason: 'Choose at least one workspace.' };
  }

  return { ok: true };
}

/**
 * The grant, in the shape the broker enforces.
 *
 * The supervisor rung carries no workspace list at all — not an empty one, and
 * not every id. Storing a list there would imply it was consulted.
 */
export function toGrant(ask, choice) {
  const scope = ask.level === 'none' ? SCOPE.workspace : choice.scope;
  const workspaces =
    scope === SCOPE.supervisor
      ? []
      : scope === SCOPE.selected
        ? [...choice.workspaces]
        : [choice.workspaceId].filter(Boolean);

  return {
    scope,
    workspaces,
    tools: { workspace: [...ask.workspace], supervisor: scope === SCOPE.supervisor ? [...ask.supervisor] : [] },
    // Recorded because it is a materially broader promise than the same list
    // frozen at install, and the screen has to be able to say which was given.
    includesFutureWorkspaces: scope === SCOPE.supervisor,
  };
}

/** Plain sentences for each rung, so the ladder reads as an escalation. */
export function scopeLabel(scope, { workspaceName = 'this workspace' } = {}) {
  if (scope === SCOPE.workspace) return `${workspaceName} only`;
  if (scope === SCOPE.selected) return 'Selected workspaces';
  return 'All workspaces, including ones you create later';
}

/**
 * The install screen's state.
 *
 * `workspaceName` is only for the label; the grant itself is built from ids.
 */
export function useExtensionGrant({ manifest, workspaceId = '', workspaces = [] } = {}) {
  const ask = computed(() => describeAsk(manifest));
  const scopes = computed(() => availableScopes(ask.value));

  // Starts on the narrowest rung it can. A default that pre-selects the widest
  // grant is a default that gets accepted.
  const scope = ref(scopes.value[0] ?? SCOPE.workspace);
  const selected = ref([]);

  const choice = computed(() => ({ scope: scope.value, workspaces: selected.value, workspaceId }));
  const validity = computed(() => validateGrant(ask.value, choice.value));

  return {
    ask,
    scopes,
    scope,
    selected,
    workspaces,
    valid: computed(() => validity.value.ok),
    problem: computed(() => (validity.value.ok ? '' : validity.value.reason)),
    label: computed(() => scopeLabel(scope.value)),
    /** Only ever called once the choice is valid. */
    grant: computed(() => toGrant(ask.value, choice.value)),
  };
}
