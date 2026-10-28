// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsent

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"regexp"
)

// PKCEMethod is the one code_challenge_method both servers accept (RFC 7636).
// "plain" is refused: it protects nothing a stolen code does not already give.
const PKCEMethod = "S256"

var (
	ErrCodeChallengeMethod    = errors.New("invalid_request: code_challenge_method must be S256")
	ErrCodeChallengeMalformed = errors.New("invalid_request: code_challenge must be the base64url SHA-256 of the code_verifier")
)

// An S256 challenge is 32 bytes in unpadded base64url; a verifier is 43 to
// 128 unreserved characters (RFC 7636 §4.1).
var (
	s256Challenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	codeVerifier  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// CheckCodeChallenge validates the PKCE parameters of an authorization
// request. A request with neither is allowed, so clients without PKCE keep
// working; one with a method needs a challenge, and the method must be S256.
func CheckCodeChallenge(challenge, method string) error {
	if challenge == "" && method == "" {
		return nil
	}
	if method != PKCEMethod {
		return ErrCodeChallengeMethod
	}
	if !s256Challenge.MatchString(challenge) {
		return ErrCodeChallengeMalformed
	}
	return nil
}

// CodeVerifierMatches says whether a token request's code_verifier answers the
// challenge its authorization code was issued for. A code issued without one
// takes no verifier either, so a verifier cannot be sent to a code that was
// never bound to it.
func CodeVerifierMatches(challenge, verifier string) bool {
	if challenge == "" {
		return verifier == ""
	}
	if !codeVerifier.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}
