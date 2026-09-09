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

/** Breathing room between the trigger and the menu, and from the viewport edge. */
export const POPOVER_GAP = 8;
export const POPOVER_MARGIN = 8;

/**
 * The tallest the menu may grow, and the least it may be squeezed to.
 *
 * The ceiling keeps a long list from becoming a full-height wall; the floor
 * stops a trigger jammed against an edge from producing a menu too short to
 * show anything, which is worse than one that overhangs slightly.
 */
export const MAX_POPOVER_HEIGHT = 420;
export const MIN_POPOVER_HEIGHT = 180;

/**
 * Where to put the menu, in viewport coordinates.
 *
 * The menu is rendered into `<body>` and positioned fixed rather than laid out
 * next to the button, because the workspace cards live inside a scrolling
 * container: an absolutely positioned menu is clipped by any ancestor that
 * scrolls, and one opening upward from a card near the top edge simply loses
 * the part that overflows — the heading and the first models with it. Nothing
 * about that is visible in a short list, which is why it survived a two-model
 * agent.
 *
 * Above the trigger by preference, since that is where a menu on a card has
 * room, flipping below when the list does not fit above. The height it may use
 * comes back with the position: a fixed cap would go on scrolling a list that
 * had a screenful of empty space beneath it, which is the same complaint as
 * being clipped — the models are there and you cannot see them.
 */
export function popoverPosition(anchor, menu, viewport) {
  // The menu may not have been measured yet — it is placed once it exists, and
  // the first pass runs before there is anything to measure. The viewport is
  // always known, so it is taken as given rather than defended against.
  const menuWidth = menu?.width || 0;
  const menuHeight = menu?.height || 0;
  const { width: viewWidth, height: viewHeight } = viewport;

  const roomAbove = anchor.top - POPOVER_GAP - POPOVER_MARGIN;
  const roomBelow = viewHeight - anchor.bottom - POPOVER_GAP - POPOVER_MARGIN;

  // Above while the whole list fits there; otherwise whichever side has more
  // room. Flipping to a side that also overflows would trade a clipped top for
  // a clipped bottom and gain nothing.
  const above = menuHeight <= roomAbove || roomAbove >= roomBelow;
  const available = Math.max(above ? roomAbove : roomBelow, MIN_POPOVER_HEIGHT);

  const maxHeight = Math.min(MAX_POPOVER_HEIGHT, available);
  const height = Math.min(menuHeight || maxHeight, maxHeight);

  let top = above ? anchor.top - POPOVER_GAP - height : anchor.bottom + POPOVER_GAP;
  // Clamped so a list too long for the screen is pinned rather than starting
  // above the fold, where its first rows could not be reached at all.
  top = Math.min(Math.max(top, POPOVER_MARGIN), Math.max(POPOVER_MARGIN, viewHeight - height - POPOVER_MARGIN));

  // Right edges aligned, the way an absolutely positioned `right-0` menu sat.
  const rightmost = Math.max(POPOVER_MARGIN, viewWidth - menuWidth - POPOVER_MARGIN);
  const left = Math.min(Math.max(anchor.right - menuWidth, POPOVER_MARGIN), rightmost);

  return { top, left, maxHeight, placement: above ? 'above' : 'below' };
}

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
      // A report that names some other model settles the question — the switch
      // is no longer outstanding, and what just arrived is the truth.
      //
      // What it does *not* establish is why. The backend republishes on a
      // changed config option or a changed list as well as on a switch, so this
      // fires both for an agent that declined and for an unrelated update that
      // happened to land mid-flight. Claiming a refusal would be wrong in the
      // second case, and a later confirming report would then contradict it.
      // So this says only what is known: which model is running now.
      if (pendingModelId.value && agentModels.value?.currentModel) {
        pendingModelId.value = '';
        const name = currentModelName(agentModels.value);
        error.value = `The agent is running ${name}`;
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
    if (!modelId) return;

    // Choosing the model that is already running is how an outstanding switch
    // is called off: there is nothing to ask for, but there is something to
    // stop waiting for. Without this the picker went on showing the abandoned
    // choice until the timeout, with no way to take it back.
    if (modelId === agentModels.value?.currentModel) {
      pendingModelId.value = '';
      return;
    }

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
