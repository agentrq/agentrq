// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// GitLab's mark, as an SVG path on a 24×24 box.
export const GITLAB_MARK = 'm23.6 9.593-.034-.086L20.3.981a.851.851 0 0 0-.336-.405.875.875 0 0 0-1 .054.875.875 0 0 0-.29.44l-2.206 6.748H7.538L5.332.97a.857.857 0 0 0-.29-.441.875.875 0 0 0-1-.054.859.859 0 0 0-.336.405L.433 9.502l-.032.086a6.066 6.066 0 0 0 2.012 7.01l.011.009.03.021 4.976 3.727 2.462 1.863 1.5 1.132a1.009 1.009 0 0 0 1.219 0l1.5-1.132 2.461-1.863 5.006-3.749.013-.01a6.068 6.068 0 0 0 2.009-7.003z';

// Where a GitLab connect started, kept for the callback page to return to.
export const GITLAB_RETURN_KEY = 'agentrq.gitlabConnectReturn';

// The path the callback page returns to: the stored one, when it is a path
// of this app, else home. Read once.
export function takeGitLabReturn() {
  let to = null;
  try {
    to = sessionStorage.getItem(GITLAB_RETURN_KEY);
    sessionStorage.removeItem(GITLAB_RETURN_KEY);
  } catch {
    // Storage off.
  }
  return typeof to === 'string' && to.startsWith('/') && !to.startsWith('//') ? to : '/';
}
