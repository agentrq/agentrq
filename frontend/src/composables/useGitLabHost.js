// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref } from 'vue';

import { getGitLabConnection } from '../api';
import { DEFAULT_GITLAB_HOST } from './useSkills';

// The server's GitLab host, asked once: it changes only when the server
// restarts.
const host = ref(DEFAULT_GITLAB_HOST);
let asked = null;

/** The GitLab host links to import skills from may name, as a ref. */
export function useGitLabHost() {
  if (!asked) {
    asked = getGitLabConnection()
      .then((rs) => {
        host.value = rs?.host || DEFAULT_GITLAB_HOST;
      })
      .catch(() => {
        // The hint falls back to gitlab.com; ask again next time.
        asked = null;
      });
  }
  return host;
}

/** Forgets the answer, for tests. */
export function resetGitLabHost() {
  asked = null;
  host.value = DEFAULT_GITLAB_HOST;
}
