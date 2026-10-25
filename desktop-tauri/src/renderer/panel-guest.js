// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The bridge declares `sidePanel.guest: 'tauri'`; this is that guest, put in
// place before desktop's renderer can mount a side panel.
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { registerPanelGuest } from '@app/composables/usePanelGuest'
import { createTauriGuest } from '../bridge/panel-guest.js'

registerPanelGuest('tauri', () => createTauriGuest({ invoke, listen }))
