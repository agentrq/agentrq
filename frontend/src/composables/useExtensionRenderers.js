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
 * ## Claimed once, asked per block
 *
 * The list of languages is read once and refreshed when extensions change,
 * because it is consulted for every message body and a bridge call per message
 * would be a call per keystroke in the reply box. The *rendering* is per block,
 * because that is a different question with a different answer each time.
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

export function useExtensionRenderers({ surfaces = useExtensionSurfaces(), workspaceId = '' } = {}) {
  const entries = ref([]);

  const languages = computed(() => entries.value.map((entry) => entry.language).filter(Boolean));

  /** Read what is claimed now. Cheap, and inert without a bridge. */
  async function load(context = {}) {
    if (!surfaces.available) return;
    entries.value = await surfaces.entriesFor('code-block', { workspaceId, ...context });
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
