// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { computed, nextTick, ref, toValue, watch } from 'vue';
import * as api from '../api';
import { skillSeenByAgents } from './useSkills';

/**
 * The `/` menu in the composer: which of the agent's slash commands a
 * half-typed message is asking for, and what accepting one does to the text.
 *
 * The commands are the connected agent's — an ACP agent advertises what it
 * accepts (`/init`, `/compact`, `/review`, …) and revises the list as context
 * makes them relevant — and the workspace's skills, which any agent can load.
 * With neither there is no menu at all.
 *
 * The logic lives here rather than in the view because the view is a large
 * component wired to a router, a store and network calls, and the project has
 * no component-test harness. What is worth testing is the matching and the
 * text-editing, and both are here.
 */

/** Whitespace ends the command name, so it is what closes the menu. */
const WHITESPACE = /\s/;

/**
 * The command name being typed, or null when the text is not asking for one.
 *
 * A message is asking for a command only while the caret is still inside the
 * first token: `/comp` is, `/compact do the thing` is not, because by then the
 * command has been chosen and what follows is its argument.
 */
export function commandQuery(text) {
  if (typeof text !== 'string' || !text.startsWith('/')) return null;
  const name = text.slice(1);
  if (WHITESPACE.test(name)) return null;
  return name;
}

/**
 * The commands worth offering for what has been typed, best first.
 *
 * Commands that *start* with the query come first, because that is what typing
 * a prefix means; the rest are those that merely contain it, which is what
 * makes `/pact` still find `compact` rather than leaving the human staring at
 * an empty menu. Within each group the order the agent gave is kept — it knows
 * which of its commands matter more than we do.
 *
 * Matching is case-insensitive so that typing `/Com` still finds `compact`,
 * even though sending it needs the name exactly as advertised — which is what
 * accepting from this menu produces.
 */
export function matchCommands(commands, query) {
  if (!Array.isArray(commands)) return [];
  const usable = commands.filter((c) => c && typeof c.name === 'string' && c.name !== '');
  if (!query) return usable;

  const wanted = query.toLowerCase();
  const starts = [];
  const contains = [];
  for (const command of usable) {
    const name = command.name.toLowerCase();
    if (name.startsWith(wanted)) starts.push(command);
    else if (name.includes(wanted)) contains.push(command);
  }
  return [...starts, ...contains];
}

/**
 * The message text after choosing a command.
 *
 * A command that takes an argument gets a trailing space, so the caret is
 * already where the argument goes — and, incidentally, so the menu closes,
 * since a space ends the first token. One that takes no argument is left bare
 * and ready to send.
 */
export function applyCommand(command) {
  return command?.input?.hint || command?.hint || command?.skill ? `/${command.name} ` : `/${command.name}`;
}

/**
 * The workspace's skills as menu entries: only those its agents can load, so
 * the menu never offers a skill the agent would not find. A skill takes the
 * rest of the message as what to do with it, hence the trailing space.
 */
export function skillCommands(skills, workspaceId) {
  if (!Array.isArray(skills)) return [];
  return skills
    .filter((s) => s && typeof s.name === 'string' && s.name !== '' && skillSeenByAgents(s, workspaceId))
    .map((s) => ({ name: s.name, description: s.description || '', skill: true }));
}

/**
 * The team skills the workspace is opted into as menu entries: those `on` and
 * not losing to an account skill of the same name (`clash`), the ones its
 * agents load. `team` marks one as the team's in the menu.
 */
export function teamSkillCommands(skills) {
  if (!Array.isArray(skills)) return [];
  return skills
    .filter((s) => s && typeof s.name === 'string' && s.name !== '' && s.on === true && !s.clash)
    .map((s) => ({ name: s.name, description: s.description || '', skill: true, team: true }));
}

/**
 * Two lists as one menu, the first winning a name both have: an agent's own
 * command and a skill of the same name are one `/name` to type.
 */
export function mergeCommands(...lists) {
  const seen = new Set();
  const merged = [];
  for (const list of lists) {
    for (const command of toValue(list) || []) {
      if (!command?.name || seen.has(command.name)) continue;
      seen.add(command.name);
      merged.push(command);
    }
  }
  return merged;
}

/**
 * The workspace's skills as menu entries, its own and then its team skills,
 * loaded again whenever the workspace changes. Only the newest load may write,
 * so a quick switch cannot leave the previous workspace's skills behind. A
 * failed load leaves the menu without skills rather than reporting it: the
 * composer still works without them. A failed team load leaves only the team
 * skills out.
 */
export function useSkillCommands(
  workspaceId,
  { load = (id) => api.searchWorkspaceSkills(id), loadTeam = (id) => api.listWorkspaceTeamSkills(id) } = {}
) {
  const commands = ref([]);
  let seq = 0;
  watch(
    () => toValue(workspaceId),
    async (id) => {
      const mine = ++seq;
      commands.value = [];
      if (!id) return;
      try {
        const team = Promise.resolve()
          .then(() => loadTeam(id))
          .catch(() => null);
        const [res, teamRes] = await Promise.all([load(id), team]);
        if (mine === seq) commands.value = mergeCommands(skillCommands(res?.skills, id), teamSkillCommands(teamRes?.skills));
      } catch {
        // Already empty.
      }
    },
    { immediate: true }
  );
  return commands;
}

/**
 * A `/name` being typed at the caret, anywhere in the text: after its start or
 * whitespace, so a path or URL (`a/b`, `https://`) never opens the menu.
 */
const SKILL_TOKEN = /(^|\s)\/([^\s/]*)$/;

/**
 * Where the `/name` at the caret starts and ends, and the name typed up to the
 * caret; null when the caret is not in one. A caret inside a word takes the
 * whole word, so choosing a skill never leaves half the old name behind.
 */
export function skillTokenAt(text, caret) {
  if (typeof text !== 'string') return null;
  const at = Number.isInteger(caret) ? Math.min(Math.max(caret, 0), text.length) : text.length;
  const m = SKILL_TOKEN.exec(text.slice(0, at));
  if (!m) return null;
  const rest = /^[^\s/]*/.exec(text.slice(at))[0];
  return { start: m.index + m[1].length, end: at + rest.length, query: m[2] };
}

/**
 * The text after choosing a skill for the `/name` at the caret, and where the
 * caret goes: just past the name and the one space after it, where what to do
 * with the skill is typed.
 */
export function applyInlineSkill(command, text, caret) {
  const current = typeof text === 'string' ? text : '';
  const at = Number.isInteger(caret) ? Math.min(Math.max(caret, 0), current.length) : current.length;
  const token = skillTokenAt(current, at) ?? { start: at, end: at };
  const after = current.slice(token.end);
  const name = `/${command.name}`;
  const space = /^\s/.test(after) ? '' : ' ';
  return {
    text: current.slice(0, token.start) + name + space + after,
    caret: token.start + name.length + 1,
  };
}

/** What moves a textarea's caret, each a reason to read it again. */
const CARET_EVENTS = ['input', 'keyup', 'click', 'focus'];

/**
 * The workspace's skills offered for a `/name` typed anywhere in a textarea,
 * not only at its start: the New Task description and a preset's message.
 * Choosing one writes the text and puts the caret after the name itself, so
 * the caller only hands over its keydown and the menu's clicks.
 *
 * @param {object} options
 * @param {import('vue').Ref<string>} options.text  the textarea's contents, writable
 * @param {import('vue').Ref<string>|(() => string)} options.workspaceId
 * @param {import('vue').Ref<HTMLTextAreaElement|null>} options.el  the textarea
 */
export function useInlineSkillMenu({ text, workspaceId, el, load, loadTeam }) {
  const caret = ref(null);
  const sync = () => {
    const node = toValue(el);
    if (node) caret.value = node.selectionStart;
  };
  watch(
    () => toValue(el),
    (node, old) => {
      for (const name of CARET_EVENTS) {
        old?.removeEventListener(name, sync);
        node?.addEventListener(name, sync);
      }
    },
    { immediate: true }
  );
  // Text set from code (a preset, dictation) moves the caret with no event.
  watch(() => toValue(text), () => nextTick(sync));

  let placed = null;
  const menu = useSlashCommands({
    text,
    commands: useSkillCommands(workspaceId, { ...(load && { load }), ...(loadTeam && { loadTeam }) }),
    query: (current) => skillTokenAt(current, caret.value)?.query ?? null,
    apply: (command, current) => {
      const next = applyInlineSkill(command, current, caret.value);
      placed = next.caret;
      return next.text;
    },
  });

  function write(next) {
    text.value = next;
    caret.value = placed;
    nextTick(() => {
      const node = toValue(el);
      node?.focus();
      node?.setSelectionRange?.(placed, placed);
    });
  }

  /** Chooses a skill, for a click on the menu; false when the menu is shut. */
  function accept(command) {
    const next = menu.accept(command);
    if (next === null) return false;
    write(next);
    return true;
  }

  /** The textarea's keydown; true when the menu took the key. */
  function handleKeydown(event) {
    const claimed = menu.handleKeydown(event);
    if (!claimed) return false;
    event.preventDefault();
    if (claimed.text !== null) write(claimed.text);
    return true;
  }

  return { ...menu, caret, accept, handleKeydown };
}

/**
 * @param {object} options
 * @param {import('vue').Ref<string>|(() => string)} options.text  the composer's contents
 * @param {import('vue').Ref<Array>|(() => Array)} options.commands  what the agent advertises
 * @param {(text: string) => string|null} [options.query]  what is being typed, or null for no menu
 * @param {(command: object, text: string) => string} [options.apply]  the text after choosing one
 * @param {(command: object) => void} [options.onAccept]  told of each command chosen
 */
export function useSlashCommands({ text, commands, query: queryOf = commandQuery, apply = applyCommand, onAccept = () => {} }) {
  /**
   * The text the menu was last dismissed for.
   *
   * Dismissing has to be remembered against *something*, or Escape would be
   * undone by the next keystroke — and accepting a command would leave the menu
   * open over the very command it just inserted. Keying it to the text means
   * typing anything else brings the menu back, which is what a human who
   * changed their mind expects.
   */
  const dismissedFor = ref(null);

  const query = computed(() => queryOf(toValue(text)));
  const matches = computed(() =>
    query.value === null ? [] : matchCommands(toValue(commands), query.value)
  );

  /**
   * Open only when there is something to show. An empty menu is worse than no
   * menu: it covers the composer to say nothing.
   */
  const open = computed(
    () => matches.value.length > 0 && toValue(text) !== dismissedFor.value
  );

  const selected = ref(0);

  // Whatever was highlighted has no meaning once the list changes underneath
  // it, so the best match is highlighted again rather than whichever row
  // happens to sit at the old index.
  watch(matches, () => {
    selected.value = 0;
  });

  /** The highlighted command, or null when the menu is closed. */
  const active = computed(() => (open.value ? matches.value[selected.value] ?? null : null));

  /** Moves the highlight, wrapping, so holding one arrow key cannot dead-end. */
  function move(delta) {
    if (!open.value) return;
    const count = matches.value.length;
    selected.value = (selected.value + delta + count) % count;
  }

  function highlight(index) {
    if (index >= 0 && index < matches.value.length) selected.value = index;
  }

  /** Closes the menu for the current text, until it changes again. */
  function dismiss() {
    dismissedFor.value = toValue(text);
  }

  /**
   * The text after accepting a command, or null when there is nothing to
   * accept — which is how the caller knows the keystroke was not the menu's and
   * should do whatever it normally does.
   */
  function accept(command = active.value) {
    if (!open.value || !command) return null;
    const applied = apply(command, toValue(text));
    dismissedFor.value = applied;
    onAccept(command);
    return applied;
  }

  /**
   * The composer's keydown, when the menu wants it.
   *
   * Returns null for every key the menu is not claiming, which is *all* of them
   * while it is shut — the caret keys have to keep moving the caret, Escape has
   * to stay the composer's, and Enter has to keep doing whatever Enter does.
   * That is the whole reason this decides rather than the template: a
   * `@keydown.down.prevent` in the markup takes the key whether the menu is
   * open or not.
   *
   * A claimed key comes back as `{ text }` — the new composer contents when the
   * keystroke chose a command, and null when it only moved the highlight.
   */
  function handleKeydown(event) {
    if (!open.value) return null;

    switch (event.key) {
      case 'ArrowDown':
        move(1);
        return { text: null };
      case 'ArrowUp':
        move(-1);
        return { text: null };
      case 'Escape':
        dismiss();
        return { text: null };
      case 'Tab':
        return { text: accept() };
      case 'Enter':
        // Cmd/Ctrl-Enter sends, and always did; a modifier means the keystroke
        // was never aimed at the menu. Shift-Enter is a newline for the same
        // reason.
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return null;
        return { text: accept() };
      default:
        return null;
    }
  }

  return { open, matches, selected, active, move, highlight, dismiss, accept, handleKeydown };
}
