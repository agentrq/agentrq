// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import "os"

// restartArgs are the arguments the replacement is started with.
//
// This process's own, minus the program name. A daemon told to serve one
// profile must come back serving that profile, and rebuilding the arguments
// from configuration would quietly drop whatever was passed on the command
// line.
func restartArgs() []string {
	if len(os.Args) < 2 {
		return nil
	}
	return append([]string(nil), os.Args[1:]...)
}
