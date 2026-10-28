// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsent

import (
	"strings"
	"testing"
)

// A verifier and its S256 challenge, worked out independently of this code.
const (
	testVerifier  = "dBjftJeZ4CVP-mJ0kjHnc6Pe_6e3ITt5FdeB7-xSjsk"
	testChallenge = "Qllz9EE2koVDgFtnbz1fqtEX4WJlP3FUeE6J4yP4c5s"
)

func TestCheckCodeChallenge(t *testing.T) {
	for name, tc := range map[string]struct {
		challenge, method string
		want              error
	}{
		"no PKCE at all":             {"", "", nil},
		"S256":                       {testChallenge, "S256", nil},
		"plain is refused":           {testChallenge, "plain", ErrCodeChallengeMethod},
		"no method means plain":      {testChallenge, "", ErrCodeChallengeMethod},
		"a method with no challenge": {"", "S256", ErrCodeChallengeMalformed},
		"too short":                  {testChallenge[:42], "S256", ErrCodeChallengeMalformed},
		"padded":                     {testChallenge + "=", "S256", ErrCodeChallengeMalformed},
		"standard base64":            {testChallenge[:42] + "+", "S256", ErrCodeChallengeMalformed},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CheckCodeChallenge(tc.challenge, tc.method); got != tc.want {
				t.Errorf("CheckCodeChallenge(%q, %q) = %v, want %v", tc.challenge, tc.method, got, tc.want)
			}
		})
	}
}

func TestCodeVerifierMatches(t *testing.T) {
	for name, tc := range map[string]struct {
		challenge, verifier string
		want                bool
	}{
		"its verifier":                          {testChallenge, testVerifier, true},
		"another verifier":                      {testChallenge, strings.Repeat("a", 43), false},
		"no verifier for a bound code":          {testChallenge, "", false},
		"a verifier with a forbidden character": {testChallenge, testVerifier[:42] + "/", false},
		"a verifier too long":                   {testChallenge, strings.Repeat("a", 129), false},
		"no challenge and no verifier":          {"", "", true},
		"a verifier for a code never bound":     {"", testVerifier, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CodeVerifierMatches(tc.challenge, tc.verifier); got != tc.want {
				t.Errorf("CodeVerifierMatches(%q, %q) = %v, want %v", tc.challenge, tc.verifier, got, tc.want)
			}
		})
	}
}
