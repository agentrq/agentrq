// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package main

import "golang.org/x/sys/windows"

// isWindowsAdmin reports whether this process is running elevated.
//
// The equivalent of the uid 0 check on Unix: a daemon that spawns agents and
// accepts remote keystrokes should hold as little privilege as it can.
func isWindowsAdmin() bool {
	token := windows.Token(0) // the current process token
	return token.IsElevated()
}
