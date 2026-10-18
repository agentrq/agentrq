// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"crypto/sha1" //nolint:gosec // a name-based UUID, not a secret
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

// conversationNamespace names the claude-code conversations this daemon
// starts, so the id derived from a session is not anybody else's.
var conversationNamespace = [16]byte{
	0x6f, 0x1d, 0x3c, 0x52, 0x8a, 0x07, 0x4e, 0x91,
	0xb2, 0x5c, 0x0e, 0x6a, 0x73, 0xd4, 0x19, 0xe8,
}

// ConversationID is the claude-code conversation a session keeps across
// restarts: a name-based UUID (version 5) of the session id, so the note
// that survives a restart needs to carry nothing more to resume it.
func ConversationID(sessionID uint64) string {
	var name [8]byte
	binary.BigEndian.PutUint64(name[:], sessionID)
	h := sha1.New() //nolint:gosec
	h.Write(conversationNamespace[:])
	h.Write(name[:])
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = u[6]&0x0f | 0x50
	u[8] = u[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

// conversationArgs are the arguments that tie a claude-code session to its
// conversation: --resume it when restoring one that was saved, otherwise
// start it under the id a later restore will look for. A restore with nothing
// saved — an agent that never got a message, or one an older daemon launched
// — starts fresh, since --resume of a conversation that is not there fails.
func (s *Supervisor) conversationArgs(req Request) []string {
	if req.Kind != KindClaudeCode {
		return nil
	}
	id := ConversationID(req.ID)
	if req.Resume && s.conversationSaved(id) {
		return []string{"--resume", id}
	}
	return []string{"--session-id", id}
}

// conversationSaved reports whether claude-code has a conversation by that id,
// in whichever project folder it filed it under.
func (s *Supervisor) conversationSaved(id string) bool {
	dir := s.ClaudeDir
	if dir == "" {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, ".claude")
	}
	found, _ := filepath.Glob(filepath.Join(dir, "projects", "*", id+".jsonl"))
	return len(found) > 0
}
