// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! The pure logic of the AgentRQ desktop app, ported from `desktop/src/main`.
//!
//! Nothing here depends on Tauri. Every side effect (disk, network, the OS) is
//! a trait the caller implements, so the rules can be tested without a window.

pub mod app_icon;
pub mod attachment_store;
pub mod deep_link;
pub mod emoji_panel;
pub mod files;
pub mod identity;
pub mod links;
pub mod notifications;
pub mod profiles;
pub mod server_config;
pub mod side_panel;
pub mod sse;
pub mod tray;
pub mod uri;
pub mod window_state;
pub mod window_view;
