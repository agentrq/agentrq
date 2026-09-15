// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import "runtime"

// exeSuffix is the extension an executable carries on this platform.
//
// Used by the tests to build a target binary that looks like a real one. The
// production path never needs it: the extension comes from the binary being
// replaced, which already has whatever this platform requires.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
