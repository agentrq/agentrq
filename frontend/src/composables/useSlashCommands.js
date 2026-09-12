// Copyright 2026 Contextual, Inc. https://agentrq.com

import { computed, ref, toValue, watch } from 'vue';

/**
 * The `/` menu in the composer: which of the agent's slash commands a
 * half-typed message is asking for, and what accepting one does to the text.
 *
 * The commands come from the connected agent, not from us — an ACP agent
 * advertises what it accepts (`/init`, `/compact`, `/review`, …) and revises
 * the list as context makes them relevant. A workspace whose agent advertises
 * nothing gets no menu at all, which is every agent that is not an ACP one.
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
  return command?.input?.hint || command?.hint ? `/${command.name} ` : `/${command.name}`;
}

/**
 * @param {object} options
 * @param {import('vue').Ref<string>|(() => string)} options.text  the composer's contents
 * @param {import('vue').Ref<Array>|(() => Array)} options.commands  what the agent advertises
 */
export function useSlashCommands({ text, commands }) {
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

  const query = computed(() => commandQuery(toValue(text)));
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
    const applied = applyCommand(command);
    dismissedFor.value = applied;
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
