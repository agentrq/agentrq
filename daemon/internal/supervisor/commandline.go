// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import "strings"

// CommandLine spells a command the daemon runs in dir the way a shell would
// take it, for the terminal: "$ cd <dir> && <argv>". It is only ever shown,
// never run, so the quoting is for a reader rather than for any one shell.
func CommandLine(dir string, argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = quoteArg(a)
	}
	return "$ cd " + quoteArg(dir) + " && " + strings.Join(quoted, " ")
}

// quoteArg leaves a plain word as it is and single-quotes anything else, so a
// folder with a space in it still reads as one argument.
func quoteArg(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@+,%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
