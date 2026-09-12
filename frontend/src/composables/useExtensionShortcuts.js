// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { computed, ref } from 'vue';

import { isTypingTarget } from './useKeyboardShortcuts';

/**
 * Keyboard shortcuts for extensions, under a prefix of their own.
 *
 *     x then l   →  Linear: create issue
 *     x then s   →  Standup: today's notes
 *
 * ## Why a prefix rather than a list of forbidden letters
 *
 * The application's scheme is bare letters, and `useKeyboardShortcuts.js`
 * explains at length why: most modifier combinations never reach the page at
 * all — Cmd+N opens a window, Cmd+H hides the app — so a shortcut bound to one
 * "would simply never fire, which is worse than having no shortcut."
 *
 * That leaves single letters, of which `k n w m t ?` are already spoken for. A
 * blocklist of those fails three ways at once. It is a **tiny namespace**. It
 * **freezes the application**, because every letter an extension takes is one we
 * can never add without breaking somebody. And **extensions collide with each
 * other**, resolved by whoever installed first, which is no answer at all.
 *
 * One reserved prefix fixes all three. `x` belongs to extensions and to nothing
 * else, so the single-letter space stays entirely ours and the second key is a
 * namespace nobody else is competing for. It is also a scheme people already
 * know from `g`-then-key in Gmail and GitHub.
 *
 * ## In-app only
 *
 * The application registers Cmd+Shift+N in the *main* process, so it fires
 * whether or not the window has focus. That is a system-wide key grab, and
 * handing one to third-party code is a different order of capability from a key
 * that works while the app is in front of you. Extensions get the second kind.
 */

/** The one key reserved for extensions. */
export const PREFIX = 'x';

/**
 * How long a half-typed sequence waits for its second key.
 *
 * Long enough not to punish a deliberate pause, short enough that a stray `x`
 * does not leave the next letter typed into a menu doing something unexpected
 * several seconds later.
 */
export const SEQUENCE_TIMEOUT_MS = 2000;

/** Letters an extension may not claim, because the prefix would never reach them. */
export const RESERVED_SECOND_KEYS = Object.freeze([PREFIX]);

/**
 * Whether a second key is one an extension may have.
 *
 * A single character, and not the prefix itself — `x x` would mean "start a
 * sequence, then start another", which is not a thing a person can press.
 */
export function isBindableKey(key) {
  const value = String(key ?? '').toLowerCase();
  if (value.length !== 1) return false;
  if (RESERVED_SECOND_KEYS.includes(value)) return false;
  return /^[a-z0-9]$/.test(value)
}

/**
 * What is wrong with an extension's requested shortcuts, if anything.
 *
 * Answered from the manifest at **install**, not at runtime. A conflict
 * discovered when somebody presses a key is a key that silently does nothing,
 * and no amount of looking at the screen explains it; told at install, it is a
 * sentence naming the extension already using it.
 */
export function conflicts(requested = [], existing = []) {
  const taken = new Map(existing.map((entry) => [String(entry.key).toLowerCase(), entry.owner]));
  const problems = [];
  const seen = new Set();

  for (const shortcut of requested) {
    const key = String(shortcut?.key ?? '').toLowerCase();

    if (!isBindableKey(key)) {
      problems.push(
        key === PREFIX
          ? `"${PREFIX}" is the prefix itself and cannot also be a shortcut.`
          : `"${shortcut?.key ?? ''}" is not a key an extension can bind. Use a single letter or digit.`,
      )
      continue
    }
    if (seen.has(key)) {
      problems.push(`This extension asks for "${PREFIX} ${key}" twice.`)
      continue
    }
    seen.add(key)

    const owner = taken.get(key)
    if (owner) problems.push(`"${PREFIX} ${key}" is already used by ${owner}.`)
  }

  return { ok: problems.length === 0, problems }
}

/** How a sequence is written on screen. */
export function formatSequence(key) {
  return `${PREFIX} then ${String(key ?? '').toLowerCase()}`
}

/**
 * The keyboard state for extension shortcuts.
 *
 * `entries` is a getter so newly installed extensions are bindable without a
 * reload, and `now`/`clearAfter` are injected so the timeout is testable without
 * waiting two seconds.
 */
export function useExtensionShortcuts({
  entries = () => [],
  onInvoke = () => {},
  onPrefix = () => {},
  now = () => Date.now(),
} = {}) {
  /** When the prefix was pressed, or 0 when no sequence is in flight. */
  const startedAt = ref(0)
  const armed = computed(() => startedAt.value !== 0)

  /** Everything bindable right now, for the help sheet and the dispatcher. */
  const bindings = computed(() =>
    entries()
      .filter((entry) => isBindableKey(entry.key))
      .map((entry) => ({
        key: String(entry.key).toLowerCase(),
        owner: entry.owner,
        id: entry.id,
        label: entry.label ?? entry.id,
        sequence: formatSequence(entry.key),
      })),
  )

  function reset() {
    startedAt.value = 0
  }

  /**
   * Offers a key event to the sequence.
   *
   * Returns whether it was consumed, so a caller can leave everything else
   * alone — the application's own bare letters must keep working, and a
   * half-typed extension sequence must not swallow them.
   */
  function handle(event) {
    if (!event?.key) return false

    // Modifiers mean the keystroke belongs to the browser or the system, and
    // typing means the letters are text. Both are the same rules the
    // application's own bare letters follow.
    const bare = !event.metaKey && !event.ctrlKey && !event.altKey
    if (!bare || isTypingTarget(event.target)) {
      reset()
      return false
    }

    const key = event.key.toLowerCase()

    if (!armed.value) {
      if (key !== PREFIX) return false
      startedAt.value = now()
      // Announced so the interface can show what is available, the way `?`
      // shows the sheet — a prefix nobody can see the options for is a prefix
      // nobody uses twice.
      onPrefix(bindings.value)
      return true
    }

    // A sequence left half-typed goes stale rather than waiting forever: a
    // stray `x` must not turn the next letter, minutes later, into a command.
    if (now() - startedAt.value > SEQUENCE_TIMEOUT_MS) {
      reset()
      return key === PREFIX ? handle(event) : false
    }

    reset()
    // Escape abandons it, which is what everybody tries first.
    if (key === 'escape') return true

    const binding = bindings.value.find((entry) => entry.key === key)
    if (!binding) return false

    onInvoke(binding)
    return true
  }

  return { armed, bindings, handle, reset }
}
