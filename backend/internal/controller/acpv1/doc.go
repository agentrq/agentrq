// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package acpv1 is AgentRQ's client for the Agent Client Protocol, version 1,
// served at /api/acp/v1 for acp-gateway in native mode.
//
// It shares no code with acpv2, on purpose: the two versions are isolated so
// that either can change, or be deleted, without touching the other. Only
// package app imports this one. app/acp_isolation_test.go enforces both.
// See docs/agents/acp-native.md.
package acpv1
