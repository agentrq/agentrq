// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import "syscall"

// detachAttr puts the background daemon in its own session so closing the
// terminal that started it does not send SIGHUP to it or its agents.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
