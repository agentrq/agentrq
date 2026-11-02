// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// detachAttr puts the background daemon in its own session so closing the
// terminal that started it does not send SIGHUP to it or its agents.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// stopProcess asks a running daemon to shut down cleanly.
func stopProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// processAlive reports whether pid is still a running process. EPERM means it
// exists but belongs to somebody else, which is still alive.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false // 0 and below address process groups, not one process
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
