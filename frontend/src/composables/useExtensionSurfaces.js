import { ref } from 'vue';

import { normaliseView } from './useExtensionView';

/**
 * The renderer's half of an extension's contributions.
 *
 * Extensions register in the main process; this asks what they registered and
 * runs one when somebody picks it. Nothing about an extension is held here — no
 * function, no module, no state — because the renderer is on a privileged
 * `app://` origin with a bridge to files, the clipboard and the shell, and
 * third-party code beside that bridge would have the whole machine through an
 * API built for this application's own UI.
 *
 * ## Fetched when the menu opens, not held in a store
 *
 * A right-click asks. That costs one bridge call on a gesture a person just
 * made, and it means an extension enabled a moment ago is on the next menu
 * rather than after a reload — which is the behaviour somebody who just
 * installed something expects to see.
 *
 * ## Anything sent across is flattened first, and it has to happen here
 *
 * This file shipped with a bug that no unit test could have caught. A task on
 * the board is a **Vue reactive proxy**, and `contextBridge` converts arguments
 * as they enter the preload's world — refusing a Proxy outright with `An object
 * could not be cloned.` The call rejected, the rejection was turned into an
 * empty list, and an installed, running extension contributed a menu row that
 * silently never appeared.
 *
 * The flattening cannot be moved into the preload or the main process: the
 * refusal happens on the way *in*, before either of them runs. The renderer is
 * the last place that can still fix it, which is why it is here.
 *
 * The other half of that lesson is the logging. The empty list is still
 * returned — a broken bridge must not stop a task's own menu opening — but it is
 * no longer returned in silence.
 *
 * ## Everything an extension draws goes through the renderer's own validator
 *
 * `normaliseView` is what decides an extension's page is drawable. It is applied
 * here rather than in each component, so there is one place a spec can be
 * refused and one place its reason comes from.
 */

/**
 * A structured-cloneable copy of whatever the caller handed over.
 *
 * JSON rather than Vue's `toRaw`, because `toRaw` only unwraps what Vue
 * wrapped: an object holding a function, a `Map`, or a proxy from anywhere else
 * is still refused. This drops whatever cannot cross instead of failing at the
 * boundary, and what an extension is given is ordinary data by design.
 */
export function plain(value) {
  try {
    return JSON.parse(JSON.stringify(value ?? {}));
  } catch {
    // A circular structure, which no task has and no extension should be sent.
    return {};
  }
}

export function useExtensionSurfaces({
  bridge = globalThis.window?.agentrq?.extensions,
  logger = globalThis.console,
} = {}) {
  // `entries` specifically, not the bridge itself: an older desktop build has an
  // extensions bridge with only the catalogue on it, and treating that as
  // present would call a method that is not there.
  const available = Boolean(bridge?.entries);

  /** What is on screen right now: a drawn view, or the reason there is not one. */
  const panel = ref(null);
  const error = ref('');
  const busy = ref(false);

  /**
   * What extensions offer for this surface and this context.
   *
   * Answers with an empty list rather than throwing, on every failure path. A
   * broken bridge must not be able to stop a task's own context menu opening —
   * the built-in items are the ones somebody actually came for.
   */
  async function entriesFor(surface, context = {}) {
    if (!available) return [];
    try {
      return (await bridge.entries(surface, plain(context))) ?? [];
    } catch (err) {
      // Reported, then swallowed. Silence here is what hid the reactive-proxy
      // failure: an installed extension contributed nothing and nothing said so.
      logger?.warn?.(`[extensions] could not read ${surface} entries:`, err?.message ?? err);
      return [];
    }
  }

  /**
   * Run one contribution and hold what it drew.
   *
   * Three outcomes, and they are deliberately distinct: a view to draw, an
   * extension that did something and drew nothing, and a failure with a reason.
   * Collapsing the middle one into either of the others would make a completed
   * action look broken.
   */
  async function invoke(target, context = {}) {
    if (!available) return;
    busy.value = true;
    error.value = '';
    panel.value = null;

    try {
      const result = await bridge.invoke(target, plain(context));
      if (!result?.ok) {
        error.value = result?.reason || 'That did not work.';
        return;
      }
      if (!result.view) return;

      const drawn = normaliseView(result.view);
      if (!drawn.ok) {
        // Named, because this is the extension author's mistake and the reason
        // is written for them: "heading" misspelled, or a node type invented.
        error.value = `${target.owner}: ${drawn.reason}`;
        return;
      }
      panel.value = { ...drawn.view, owner: target.owner, id: target.id };
    } catch (err) {
      error.value = err?.message || 'That did not work.';
    } finally {
      busy.value = false;
    }
  }

  /**
   * Run one contribution and hand the answer straight back.
   *
   * Distinct from `invoke` because that one *holds* what it drew: it is for a
   * person clicking something, so there is one panel and one error at a time.
   * A renderer is asked for many blocks while a message is being drawn, and
   * they all need their own answer — putting those through `panel` would mean
   * each block overwriting the last and an unrelated dialog opening.
   */
  async function invokeQuietly(target, context = {}) {
    if (!available) return { ok: false, reason: '' };
    try {
      const result = await bridge.invoke(target, plain(context));
      if (!result?.ok) return { ok: false, reason: result?.reason ?? '' };
      return { ok: true, view: result.view };
    } catch (err) {
      return { ok: false, reason: err?.message ?? '' };
    }
  }

  function dismiss() {
    panel.value = null;
    error.value = '';
  }

  return { available, panel, error, busy, entriesFor, invoke, invokeQuietly, dismiss };
}
