// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/** How the hook and event forms behave at the keyboard. */

/**
 * What a keydown in a form asks for: 'submit' on Cmd+Enter or Ctrl+Enter
 * (from any field, a textarea too), 'cancel' on Escape, or ''.
 */
export function formKeyAction(event) {
  if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) return 'submit';
  if (event.key === 'Escape') return 'cancel';
  return '';
}

/**
 * Puts the cursor in the first field marked invalid, or on the first radio
 * of a group with an error, so a refused form says where to look.
 */
export function focusFirstInvalid(form) {
  const field = form?.querySelector('[aria-invalid="true"], fieldset[aria-describedby] input');
  field?.focus();
}

/** The keys that submit a form, as the keyboard in use names them. */
export function submitHint(mac) {
  return mac ? '⌘ Enter' : 'Ctrl Enter';
}
