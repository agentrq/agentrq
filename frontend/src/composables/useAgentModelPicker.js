import { computed, ref, watch } from 'vue';

import { currentModelName } from './useAgentSummary';

/**
 * Choosing which model the workspace's agent runs.
 *
 * The interface never learns from its own request whether a switch happened.
 * It asks, and the agent answers later with a models notification that arrives
 * over the event stream — so what is rendered has to be "asked for" until that
 * report agrees, and must go back to the truth if it never does.
 *
 * The pure parts are exported separately because that is what the rest of the
 * app asks about — whether to draw a picker at all — and because the project
 * has no component-test harness, so anything worth pinning has to live outside
 * the SFC.
 */

/**
 * How long to wait for the agent to confirm before giving up on a switch.
 *
 * Generous on purpose: the agent may be mid-turn, and an answer that arrives
 * late is still an answer. What this bounds is the pending state, not the
 * switch — reverting only puts back what the agent last said is true.
 */
export const CONFIRMATION_TIMEOUT_MS = 30_000;

/**
 * Whether a workspace can be offered a model picker at all.
 *
 * Three separate things, and all of them have to hold. The agent has to be
 * attached, because a choice sent to nothing goes nowhere. It has to have
 * reported models, which is what excludes every client that never mentions
 * them — Claude Code among them, with no list to maintain. And it has to have
 * said it will act on being told to switch, which older gateways do not: they
 * report their models perfectly well and ignore the request, so drawing a
 * picker on the strength of the list alone would offer a control that silently
 * does nothing.
 */
export function canChooseModel(workspace) {
  if (!workspace?.agentConnected) return false;
  const models = workspace?.agentModels;
  if (models?.canSet !== true) return false;
  return Array.isArray(models.models) && models.models.length > 0;
}

/**
 * The offered models, in the groups the agent put them in.
 *
 * Grouping is the agent's own — usually the provider — and models without one
 * are collected under an unnamed group so a mixed list still renders in one
 * pass. Order is preserved rather than sorted: the agent listed them the way it
 * did, and re-ordering would put a different model under the cursor than the
 * one the agent calls first.
 */
export function modelGroups(agentModels) {
  const models = Array.isArray(agentModels?.models) ? agentModels.models : [];
  const groups = [];
  const byName = new Map();

  for (const model of models) {
    if (!model?.id) continue;
    const name = typeof model.group === 'string' ? model.group.trim() : '';
    let group = byName.get(name);
    if (!group) {
      group = { name, models: [] };
      byName.set(name, group);
      groups.push(group);
    }
    group.models.push(model);
  }
  return groups;
}

/**
 * The model to show as chosen: what was asked for, until the agent answers.
 *
 * The requested one wins while it is outstanding, because that is what the
 * human just did and showing the old value would read as the click having
 * missed. Once the report agrees — or the request is abandoned — this is the
 * agent's own answer again.
 */
export function displayedModelId(agentModels, pendingModelId) {
  return pendingModelId || agentModels?.currentModel || '';
}

/**
 * Whether the agent has confirmed the model that was asked for.
 *
 * Confirmation is only ever the agent saying the model is current. A report
 * that arrives naming something else is not silence — it is the agent
 * declining, or a second gateway answering — and either way the request is
 * over and what it says is the truth.
 */
export function isConfirmed(pendingModelId, agentModels) {
  if (!pendingModelId) return true;
  return agentModels?.currentModel === pendingModelId;
}

/**
 * The picker's state and the one action it takes.
 *
 * `workspace` is a getter rather than a value so this follows the store: the
 * confirming report arrives on the event stream and rewrites the workspace in
 * place, and a composable holding a snapshot would never see it.
 */
export function useAgentModelPicker({ workspace, selectModel, now = () => Date.now(), timeoutMs = CONFIRMATION_TIMEOUT_MS }) {
  const pendingModelId = ref('');
  const requestedAt = ref(0);
  const error = ref('');
  const sending = ref(false);

  const agentModels = computed(() => workspace()?.agentModels);
  const available = computed(() => canChooseModel(workspace()));
  const groups = computed(() => modelGroups(agentModels.value));
  const selectedId = computed(() => displayedModelId(agentModels.value, pendingModelId.value));
  const isPending = computed(() => pendingModelId.value !== '');

  /** The name to show for whatever is selected, falling back to its id. */
  const selectedName = computed(() =>
    currentModelName({ currentModel: selectedId.value, models: agentModels.value?.models }),
  );

  // The agent's report is the only thing that ends a pending switch. Watched
  // rather than checked on click, because the report may arrive at any point
  // and nothing else is looking for it.
  //
  // The whole report is watched, not the model named in it. An agent that
  // declines re-sends its models with the *same* current model, so watching
  // that value would see nothing change and leave the picker pending on a
  // switch that has already been answered — the refusal is exactly the case
  // this exists to catch. The store rebuilds the object on every event, so its
  // identity is what moves.
  watch(
    agentModels,
    () => {
      if (isConfirmed(pendingModelId.value, agentModels.value)) {
        pendingModelId.value = '';
        return;
      }
      // A report naming a different model is the agent having its own answer,
      // which settles the question as surely as agreement would.
      if (pendingModelId.value && agentModels.value?.currentModel) {
        pendingModelId.value = '';
        error.value = 'The agent stayed on a different model';
      }
    },
  );

  /**
   * Give up on a switch nothing ever confirmed.
   *
   * Called by whatever is driving the clock rather than by a timer of its own,
   * so a test can move time without waiting for it and a component can stop
   * asking when it goes away.
   */
  function expirePending() {
    if (!pendingModelId.value) return;
    if (now() - requestedAt.value < timeoutMs) return;
    pendingModelId.value = '';
    error.value = 'The agent did not confirm the change';
  }

  /**
   * Ask the agent to switch.
   *
   * The pending model is set before the request and kept whatever the request
   * answers, because a 202 is only an acknowledgement that the agent was asked.
   * A refusal is different: nothing was asked, so there is nothing to wait for.
   */
  async function choose(modelId) {
    error.value = '';
    if (!modelId || modelId === agentModels.value?.currentModel) return;

    pendingModelId.value = modelId;
    requestedAt.value = now();
    sending.value = true;
    try {
      await selectModel(modelId);
    } catch (err) {
      pendingModelId.value = '';
      error.value = err?.message || 'Could not change the model';
    } finally {
      sending.value = false;
    }
  }

  return {
    available,
    groups,
    selectedId,
    selectedName,
    isPending,
    sending,
    error,
    choose,
    expirePending,
  };
}
