// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { computed, ref } from 'vue';

import { useExtensionSurfaces } from './useExtensionSurfaces';

/**
 * Which fenced-code languages extensions have claimed, and what they draw.
 *
 * The fourth surface, and the first that changes something AgentRQ was already
 * rendering rather than adding a place of its own. A renderer claims a language
 * — `mermaid`, `vega`, whatever — and is asked to turn one fence's source into
 * a view spec.
 *
 * ## Claimed once for the whole app, asked per block
 *
 * The list of languages is **shared**, not per component. Every message body
 * consults it, a conversation is many bodies, and each one is re-rendered as it
 * is typed near — a list per `MarkdownBody` would be a bridge call per message
 * and then per keystroke. So it is read once, held here, and refreshed when
 * extensions change.
 *
 * The *rendering* is per block, because that is a different question with a
 * different answer each time.
 *
 * ## A language belongs to one extension
 *
 * Two extensions both claiming `mermaid` would mean whichever loaded first
 * wins, which is no answer at all — so the registry that holds these is unique
 * by language, and the second claim is refused at install with the first
 * extension named.
 *
 * ## Enabled per workspace is the extension's own answer
 *
 * A renderer entry may carry a `when(context)`, evaluated in the main process
 * where it can run, so an extension decides for itself which workspaces it
 * applies to. That is what "render it if it is enabled for the workspace"
 * means: not a setting AgentRQ keeps, but a question the extension answers.
 */

/**
 * What is claimed, for the whole application.
 *
 * Module state rather than per-caller, because every message body reads it and
 * they must not each ask. Exported for the tests, which need to start from
 * nothing.
 */
const shared = ref([]);

/** Which read is current, so a slow answer cannot overwrite a newer one. */
let reading = 0;

export function resetRenderers() {
  shared.value = [];
  reading = 0;
}

export function useExtensionRenderers({ surfaces = useExtensionSurfaces(), workspaceId = '' } = {}) {
  const entries = shared;

  const languages = computed(() => entries.value.map((entry) => entry.language).filter(Boolean));

  /** Read what is claimed now. Cheap, and inert without a bridge. */
  async function load(context = {}) {
    if (!surfaces.available) return;
    const token = (reading += 1);
    const found = await surfaces.entriesFor('code-block', { workspaceId, ...context });
    if (token === reading) entries.value = found;
  }

  /**
   * Ask whoever claimed this language to draw one block.
   *
   * Answers `{ ok: false }` rather than throwing on every path — this runs
   * while a message is being drawn, and an exception would take the whole
   * message with it rather than the one block that could not be rendered.
   */
  async function render(language, source, context = {}) {
    const entry = entries.value.find((candidate) => candidate.language === language);
    if (!entry) return { ok: false };

    const answer = await surfaces.invokeQuietly(
      { owner: entry.owner, id: entry.id, surface: 'code-block' },
      { workspaceId, ...context, language, source },
    );
    return answer?.ok ? { ok: true, view: answer.view } : { ok: false, reason: answer?.reason ?? '' };
  }

  return { available: surfaces.available, entries, languages, load, render };
}
