// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package pty

// systemPath is empty off Windows, which has no system-wide PATH to re-read:
// only the environment the daemon was started with.
var systemPath = func() string { return "" }
