// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Every workspace's board (a team's too), or one workspace's Board tab, with
// or without a card's chat open beside it.
const BOARD = /^(\/teams\/[^/?#]+)?\/kanban(?=[/?#]|$)|^\/workspaces\/[^/?#]+\/board(?=[/?#]|$)/;

/**
 * The board a form was opened from, to go back to once it is done; '' when it
 * was opened from anywhere else.
 *
 * @param {unknown} back the router's `history.state.back`: the previous path
 */
export function boardReturnPath(back) {
  return typeof back === 'string' && BOARD.test(back) ? back : '';
}
