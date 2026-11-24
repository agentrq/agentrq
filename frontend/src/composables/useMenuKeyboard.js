// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The keyboard in a picker's menu (#1346). Focus starts on the checked item;
 * Up/Down move it with wrap-around, Home/End jump to the ends, and typing jumps
 * to the next item whose name starts with what was typed. Enter picks, as on
 * any focused button. Escape closes and hands focus back to the trigger; Tab
 * does the same and then moves on from it. The browser scrolls a focused item
 * into view, so a long list follows along.
 */

/** How long a pause starts a new type-to-jump search. */
export const TYPEAHEAD_RESET_MS = 500;

const items = (menu) => Array.from(menu?.querySelectorAll('button:not([disabled])') ?? []);

const label = (item) => item.textContent.trim().toLowerCase();

/**
 * `menu` is the menu's ref and `close` puts focus back on the trigger; `now`
 * is the clock, a parameter so tests can move it.
 */
export function useMenuKeyboard({ menu, close, now = () => Date.now() }) {
  let typed = '';
  let typedAt = 0;

  /** Focus the checked item, else the first. */
  function focusChecked() {
    const list = items(menu.value);
    (list.find((b) => b.getAttribute('aria-checked') === 'true') ?? list[0])?.focus();
  }

  function typeahead(list, i, key) {
    const t = now();
    typed = t - typedAt > TYPEAHEAD_RESET_MS ? key : typed + key;
    typedAt = t;
    // One letter, however often pressed, moves on from the current item, so
    // repeating it walks through every item starting with it; a longer prefix
    // may still match the current one.
    const cycling = [...typed].every((c) => c === typed[0]);
    const prefix = cycling ? typed[0] : typed;
    const start = cycling ? i + 1 : Math.max(i, 0);
    for (let n = 0; n < list.length; n++) {
      const item = list[(start + n) % list.length];
      if (label(item).startsWith(prefix)) return item;
    }
    return null;
  }

  function onKeydown(event) {
    const list = items(menu.value);
    const i = list.indexOf(document.activeElement);
    let target = null;
    const isTyping = event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey;
    // Any other key ends a search, so the next letter starts a new one.
    if (!isTyping) typed = '';

    switch (event.key) {
      case 'Escape':
        event.preventDefault();
        close();
        return;
      case 'Tab':
        close();
        return;
      case 'ArrowDown':
        target = list[(i + 1) % list.length];
        break;
      case 'ArrowUp':
        // From nothing focused, Up starts at the bottom.
        target = list[((i < 0 ? 0 : i) - 1 + list.length) % list.length];
        break;
      case 'Home':
        target = list[0];
        break;
      case 'End':
        target = list[list.length - 1];
        break;
      default:
        if (!isTyping) return;
        target = typeahead(list, i, event.key.toLowerCase());
        if (!target) return;
    }
    event.preventDefault();
    target?.focus();
  }

  return { focusChecked, onKeydown };
}
