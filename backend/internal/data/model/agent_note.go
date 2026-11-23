// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package model

import (
	"strconv"

	"github.com/cespare/xxhash/v2"
)

// AgentNoteID is the AgentNote row a note text is kept under in an account:
// xxhash64 of both, reinterpreted as int64.
func AgentNoteID(userID int64, text string) int64 {
	return int64(xxhash.Sum64String(strconv.FormatInt(userID, 10) + "\x00" + text))
}
