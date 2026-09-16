// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package machine holds the rules for enrolling a machine and recognising it
// again afterwards.
//
// Two secrets live here and they are deliberately different shapes.
//
// An **enrolment code** is read off a screen and typed by a person, so it is
// short and therefore weak. It is made safe by being single-use and expiring in
// minutes, not by its length.
//
// A **machine token** is never seen by a human, so it has no reason to be
// short. It is long, random, and stored only as a hash — a database that leaks
// should not hand over the ability to act as every enrolled machine.
package machine

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// codeAlphabet deliberately omits the characters people confuse when copying
// from a screen: 0/O, 1/I/L, and U (which turns up in unfortunate words).
//
// Excluding them costs a little entropy and buys back every support
// conversation that starts "it says my code is wrong".
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

// CodeLength is the number of characters in an enrolment code, not counting
// the separator. 8 characters over a 30-character alphabet is about 2^39
// combinations — small for a password and ample for a secret that lives for
// minutes, is single-use, and is rate limited.
const CodeLength = 8

// CodeTTL is how long an enrolment code is worth typing.
//
// Short on purpose. This is the one moment a credential crosses a human's
// hands, and a code left in a terminal's scrollback should be useless by the
// time anyone finds it.
const CodeTTL = 10 * time.Minute

// TokenBytes is the size of a machine token before encoding. 32 bytes is the
// size of the hash it is stored under; more would be decoration.
const TokenBytes = 32

// NewEnrolmentCode returns a code in the shape a person expects to type,
// grouped with a hyphen: ABCD-2345.
func NewEnrolmentCode() (string, error) {
	b := make([]byte, CodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("machine: generate enrolment code: %w", err)
	}
	out := make([]byte, 0, CodeLength+1)
	for i, v := range b {
		if i == CodeLength/2 {
			out = append(out, '-')
		}
		// Modulo bias over a 30-character alphabet is around 2%, which matters
		// for a key and not for a single-use code that expires in minutes. The
		// alternative — rejection sampling — buys nothing here and is one more
		// loop to get wrong.
		out = append(out, codeAlphabet[int(v)%len(codeAlphabet)])
	}
	return string(out), nil
}

// NormalizeCode puts a typed code into the one form everything else compares.
//
// People paste codes with spaces, type them lowercase, and leave the hyphen
// out. All three should work: the alternative is an error message that says
// the code is wrong when the person read it correctly.
func NormalizeCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	if len(s) != CodeLength {
		// Returned as-is so the caller's comparison fails. Padding or
		// truncating here would make a wrong-length code silently become a
		// different code.
		return s
	}
	return s[:CodeLength/2] + "-" + s[CodeLength/2:]
}

// NewMachineToken returns a token and the hash to store for it.
//
// The plain token is returned once, to be handed to the daemon, and is not
// recoverable afterwards. That is the point: a database that leaks should not
// hand over the ability to act as every enrolled machine.
func NewMachineToken() (token, hash string, err error) {
	b := make([]byte, TokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("machine: generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashSecret(token), nil
}

// HashSecret is how both secrets are stored.
//
// A plain SHA-256 rather than a password hash, and that is deliberate: bcrypt
// and friends exist to slow down guessing a *human-chosen* secret. These are
// 256 bits of machine-generated randomness, where guessing is not the threat
// and a slow hash would only add latency to every request the daemon makes.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SecretMatches compares a presented secret against a stored hash.
//
// Constant time, because the comparison happens on every request a daemon
// makes and a timing signal on a token check is a slow leak of the token.
func SecretMatches(storedHash, presented string) bool {
	if storedHash == "" || presented == "" {
		return false
	}
	want, err := hex.DecodeString(storedHash)
	if err != nil {
		return false
	}
	got := sha256.Sum256([]byte(presented))
	return subtle.ConstantTimeCompare(want, got[:]) == 1
}

// OnlineThreshold is how long after its last heartbeat a machine is still
// considered online.
//
// Comfortably more than the heartbeat interval, so one dropped beat on a busy
// machine does not flap the UI between online and offline.
const OnlineThreshold = 90 * time.Second

// IsOnline reports whether a machine was heard from recently enough.
//
// Derived rather than stored, and that is the whole point: a stored flag says
// "online" forever when a daemon is killed, which is exactly the moment the
// answer matters. A zero LastSeenAt — a machine enrolled but never connected —
// is offline rather than an error.
func IsOnline(lastSeen, now time.Time, threshold time.Duration) bool {
	if lastSeen.IsZero() {
		return false
	}
	// A heartbeat from the future means clock skew somewhere. Treating it as
	// online is the safe reading: the machine plainly just spoke to us.
	return !now.After(lastSeen.Add(threshold))
}

// Session states, shared by the controller, the handler and the daemon.
//
// Strings rather than an enum because they are stored in a column and sent
// over a socket; a value that survives both round trips is worth more than one
// that needs translating at each edge.
const (
	SessionStarting = "starting"
	SessionRunning  = "running"
	SessionExited   = "exited"
	SessionKilled   = "killed"
	SessionFailed   = "failed"
)

// SessionTerminal reports whether a session can still change.
func SessionTerminal(status string) bool {
	switch status {
	case SessionExited, SessionKilled, SessionFailed:
		return true
	default:
		return false
	}
}
