// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The bridge first: desktop's renderer reads window.agentrq as it loads.
import '../bridge/index.js'
import './panel-guest.js'
import '../../../desktop/src/renderer/main.js'
