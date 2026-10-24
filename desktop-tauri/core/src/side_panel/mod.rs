// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The desktop side panel: permissions and the permission store, guest rules,
//! the Extension bridge, the `agentrq-ext` scheme and the window wiring.

pub mod bridge;
pub mod ext_scheme;
pub mod guest;
pub mod permission_store;
pub mod permissions;
pub mod wire;
