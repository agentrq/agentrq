// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { computed, ref, watch } from 'vue';

/**
 * How many tasks the workspace's gateway runs at once.
 *
 * The interface never learns from its own request whether a change happened.
 * It asks, and the gateway answers later with a concurrency report that
 * arrives over the event stream — accepted, clamped to its own range, or left
 * untouched — so what is rendered has to be "asked for" until that report
 * agrees, and must go back to the truth if it never does. Exactly the shape of
 * the model picker next door, and for exactly the same reason.
 *
 * The pure parts are exported separately because that is what the rest of the
 * app asks about — whether to draw a control at all — and because the project
 * has no component-test harness, so anything worth pinning has to live outside
 * the SFC.
 */

/**
 * How long to wait for the gateway to confirm before giving up on a change.
 *
 * Shorter than the model picker's, because nothing here has to wait for a turn
 * to end: the gateway answers a set from its queue manager, not from the agent.
 * What this bounds is the pending state, not the change — reverting only puts
 * back what the gateway last said is true.
 */
export const CONFIRMATION_TIMEOUT_MS = 10_000;

/** The floor, whatever a gateway reports. A limit of 0 is a stalled queue. */
export const MIN_CONCURRENCY = 1;

/**
 * Whether a workspace has a concurrency worth showing at all.
 *
 * The gateway has to be attached, because a number from something that has
 * gone is not news about anything. And it has to have reported a limit, which
 * is what excludes every client that never mentions one — Claude Code attached
 * directly among them, which has no queue of its own to report.
 */
export function hasConcurrency(workspace) {
  if (!workspace?.agentConnected) return false;
  return concurrencyLimit(workspace?.agentConcurrency) > 0;
}

/**
 * Whether that number may also be changed.
 *
 * The extra thing over `hasConcurrency` is the gateway having said it will act
 * on being told a new limit, which older ones do not: they report their limit
 * perfectly well and ignore the request. Drawing a control on the strength of
 * the number alone would offer something that silently does nothing on every
 * deployment running a gateway from before this feature — so silence is read
 * as no, the same rule the model picker follows.
 */
export function canSetConcurrency(workspace) {
  if (!hasConcurrency(workspace)) return false;
  return workspace?.agentConcurrency?.canSet === true;
}

/** The limit in force, or 0 when nothing sensible was reported. */
export function concurrencyLimit(agentConcurrency) {
  const limit = Number(agentConcurrency?.maxConcurrency);
  return Number.isFinite(limit) && limit > 0 ? Math.floor(limit) : 0;
}

/**
 * The range a control may offer, as the gateway reported it.
 *
 * Read rather than assumed: the gateway owns its own ceiling, and a number
 * compiled in here would be wrong the first time it moved. `max` of 0 means it
 * named no ceiling, which stays 0 — a control leaves its upper bound off
 * rather than inventing one.
 *
 * The range is widened to contain the limit in force, mirroring the rule the
 * backend applies to the same numbers. It is repeated here because these two
 * values also arrive separately — the workspace payload on load, an event
 * after — and a control that cannot represent the value it is showing would
 * propose a change nobody asked for the moment it opened.
 */
export function concurrencyRange(agentConcurrency) {
  const limit = concurrencyLimit(agentConcurrency);

  const reportedMin = Number(agentConcurrency?.min);
  let min = Number.isFinite(reportedMin) && reportedMin > MIN_CONCURRENCY
    ? Math.floor(reportedMin)
    : MIN_CONCURRENCY;

  const reportedMax = Number(agentConcurrency?.max);
  let max = Number.isFinite(reportedMax) && reportedMax > 0 ? Math.floor(reportedMax) : 0;

  if (max > 0 && max < min) max = min;
  if (limit > 0) {
    if (limit < min) min = limit;
    if (max > 0 && limit > max) max = limit;
  }
  return { min, max };
}

/**
 * Bring a value inside the range, or return 0 for something that is not a
 * limit at all.
 *
 * A value the gateway would clamp is clamped here first, deliberately. The
 * gateway does answer a clamped set — it always answers — but it answers with
 * a limit that is not the one asked for, which is the same shape as a refusal
 * and is reported as one. Asking for 9000 against a ceiling of 64 would
 * therefore obey perfectly and still put an error under the control. Clamping
 * first means what is asked for is what is shown, so the report agrees.
 */
export function clampConcurrency(value, range) {
  const wanted = Math.floor(Number(value));
  if (!Number.isFinite(wanted)) return 0;

  const { min, max } = range || { min: MIN_CONCURRENCY, max: 0 };
  if (wanted < min) return min;
  if (max > 0 && wanted > max) return max;
  return wanted;
}

/**
 * Whether more is running than the limit allows.
 *
 * Not an error state, and worth saying out loud somewhere: lowering the limit
 * never interrupts a running task. They finish, and the queue stops handing out
 * new ones until the active count falls back under the limit — so this is the
 * feature working, and an interface that flagged it red would be reporting
 * correct behaviour as a fault.
 */
export function isOverLimit(agentConcurrency) {
  const limit = concurrencyLimit(agentConcurrency);
  // A limit above zero is only possible from an object, so what follows needs
  // no guard of its own — one here would be a branch nothing could ever take.
  if (limit === 0) return false;
  return (Number(agentConcurrency.active) || 0) > limit;
}

/** What the queue is doing, in words, or '' when there is nothing to say. */
export function concurrencySummary(agentConcurrency) {
  if (concurrencyLimit(agentConcurrency) === 0) return '';
  const active = Number(agentConcurrency?.active) || 0;
  const queued = Number(agentConcurrency?.queued) || 0;
  return `${active} running · ${queued} queued`;
}

/**
 * The limit to show: what was asked for, until the gateway answers.
 *
 * The requested one wins while it is outstanding, because that is what the
 * human just did and showing the old value would read as the change having
 * missed. Once the report agrees — or the request is abandoned — this is the
 * gateway's own answer again.
 */
export function displayedConcurrency(agentConcurrency, pendingValue) {
  return pendingValue || concurrencyLimit(agentConcurrency);
}

/**
 * Whether the gateway has confirmed the limit that was asked for.
 *
 * Confirmation is only ever the gateway saying that limit is in force. A report
 * naming something else is not silence — it is the gateway having clamped the
 * value, or a second gateway answering — and either way the request is over and
 * what it says is the truth.
 */
export function isConcurrencyConfirmed(pendingValue, agentConcurrency) {
  if (!pendingValue) return true;
  return concurrencyLimit(agentConcurrency) === pendingValue;
}

/**
 * The control's state and the one action it takes.
 *
 * `workspace` is a getter rather than a value so this follows the store: the
 * confirming report arrives on the event stream and rewrites the workspace in
 * place, and a composable holding a snapshot would never see it.
 */
export function useAgentConcurrency({ workspace, setConcurrency, now = () => Date.now(), timeoutMs = CONFIRMATION_TIMEOUT_MS }) {
  const pendingValue = ref(0);
  const requestedAt = ref(0);
  const error = ref('');
  const sending = ref(false);

  const concurrency = computed(() => workspace()?.agentConcurrency);
  const visible = computed(() => hasConcurrency(workspace()));
  const editable = computed(() => canSetConcurrency(workspace()));
  const range = computed(() => concurrencyRange(concurrency.value));
  const value = computed(() => displayedConcurrency(concurrency.value, pendingValue.value));
  const active = computed(() => Number(concurrency.value?.active) || 0);
  const queued = computed(() => Number(concurrency.value?.queued) || 0);
  const summary = computed(() => concurrencySummary(concurrency.value));
  const overLimit = computed(() => isOverLimit(concurrency.value));
  const isPending = computed(() => pendingValue.value !== 0);

  // The gateway's report is the only thing that ends a pending change. Watched
  // rather than checked on send, because the report may arrive at any point and
  // nothing else is looking for it.
  //
  // The whole report is watched, not the limit named in it. A gateway that
  // clamps re-sends a limit that may well be what it was already running, so
  // watching that value alone would see nothing change and leave the control
  // pending on a request that has already been answered. The store rebuilds the
  // object on every event, so its identity is what moves.
  watch(
    concurrency,
    () => {
      if (isConcurrencyConfirmed(pendingValue.value, concurrency.value)) {
        pendingValue.value = 0;
        return;
      }
      // A report naming some other limit settles the question — the change is
      // no longer outstanding, and what just arrived is the truth.
      //
      // What it does *not* establish is why. Every report is republished, and
      // a gateway reconnecting reports too — so this fires for a gateway that
      // clamped the value, and equally for a reconnect that happened to land
      // mid-flight. Claiming a refusal would be wrong in the second case. So
      // this says only what is known: the limit now in force.
      const limit = concurrencyLimit(concurrency.value);
      if (pendingValue.value && limit > 0) {
        pendingValue.value = 0;
        error.value = `The gateway is running ${limit} at a time`;
      }
    },
  );

  /**
   * Give up on a change nothing ever confirmed.
   *
   * Called by whatever is driving the clock rather than by a timer of its own,
   * so a test can move time without waiting for it and a component can stop
   * asking when it goes away.
   */
  function expirePending() {
    if (!pendingValue.value) return;
    if (now() - requestedAt.value < timeoutMs) return;
    pendingValue.value = 0;
    error.value = 'The gateway did not confirm the change';
  }

  /**
   * Ask the gateway to run a different number of tasks at once.
   *
   * The pending value is set before the request and kept whatever the request
   * answers, because a 202 is only an acknowledgement that the gateway was
   * asked. A refusal is different: nothing was asked, so there is nothing to
   * wait for.
   */
  async function choose(wanted) {
    error.value = '';
    const limit = clampConcurrency(wanted, range.value);
    if (limit === 0) return;

    // Asking for the limit already in force is how an outstanding change is
    // called off: there is nothing to ask for, but there is something to stop
    // waiting for. It is also the case the clamp above creates — a value over
    // the ceiling becomes the ceiling — and spending a round trip to be told
    // the number already on screen is worth skipping.
    if (limit === concurrencyLimit(concurrency.value)) {
      pendingValue.value = 0;
      return;
    }

    pendingValue.value = limit;
    requestedAt.value = now();
    sending.value = true;
    try {
      await setConcurrency(limit);
    } catch (err) {
      pendingValue.value = 0;
      error.value = err?.message || 'Could not change the concurrency';
    } finally {
      sending.value = false;
    }
  }

  return {
    visible,
    editable,
    range,
    value,
    active,
    queued,
    summary,
    overLimit,
    isPending,
    sending,
    error,
    choose,
    expirePending,
  };
}
