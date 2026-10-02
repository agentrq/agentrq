// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The preload of every page in the side panel.
 *
 * Set by the main process whatever the `<webview>` element asked for, so a
 * guest can never be given the app's own preload and its bridge. It exposes
 * nothing yet: a web page in the panel gets no bridge at all.
 */
