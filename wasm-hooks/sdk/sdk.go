// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

// Package sdk is the AgentRQ hook contract (ABI v1) for hooks written in Go
// and built with GOOS=wasip1 GOARCH=wasm. A hook is a command: run with
// "manifest" it prints its manifest; run with "handle" it reads a request on
// stdin and prints its answer. Main does both; a hook supplies the manifest
// and a function from a request to its answer.
package sdk

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ABIVersion is the contract version this package speaks.
const ABIVersion = 1

// Manifest is what a hook says about itself.
type Manifest struct {
	ABIVersion  int     `json:"abiVersion"`
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description string  `json:"description,omitempty"`
	Inputs      []Input `json:"inputs"`
	Events      []Event `json:"events"`
}

// Input is a value each registration of the hook provides. A hook must
// declare at least one required secret input, and check it.
type Input struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type"`
	Items    string `json:"items,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Event is one kind of event the hook writes.
type Event struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Request is the request the outside service sent. Header names are
// lowercase.
type Request struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   map[string][]string `json:"query"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

// Header is the first value of the header, or "".
func (r Request) Header(name string) string {
	if v := r.Headers[strings.ToLower(name)]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// In is one request with the registration's input values.
type In struct {
	RegistrationID string                     `json:"registrationId"`
	Request        Request                    `json:"request"`
	Inputs         map[string]json.RawMessage `json:"inputs"`
	Now            time.Time                  `json:"now"`
}

// String is a string input's value, or "".
func (in In) String(name string) string {
	var s string
	_ = json.Unmarshal(in.Inputs[name], &s)
	return s
}

// Strings is an array input's values.
func (in In) Strings(name string) []string {
	var s []string
	_ = json.Unmarshal(in.Inputs[name], &s)
	return s
}

// FAQ is a question an agent may have about an event, answered up front.
type FAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// Out is one event the hook writes. Payload is a string, used as is, or any
// value, sent as JSON.
type Out struct {
	Name     string `json:"name"`
	DedupKey string `json:"dedupKey,omitempty"`
	Payload  any    `json:"payload,omitempty"`
	FAQ      []FAQ  `json:"faq,omitempty"`
}

// Result is the hook's answer.
type Result struct {
	Accept bool   `json:"accept"`
	Reason string `json:"reason,omitempty"`
	Events []Out  `json:"events,omitempty"`
}

// Accept answers that the request is genuine, with the events it stands
// for; none is fine when it is of no interest.
func Accept(events ...Out) Result { return Result{Accept: true, Events: events} }

// Reject answers that the request failed validation.
func Reject(reason string) Result { return Result{Reason: reason} }

// Main runs the hook as the server calls it, and exits.
func Main(m Manifest, handle func(In) Result) {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, m, handle))
}

func run(args []string, stdin io.Reader, stdout io.Writer, m Manifest, handle func(In) Result) int {
	command := ""
	if len(args) > 1 {
		command = args[1]
	}
	var out any
	switch command {
	case "manifest":
		m.ABIVersion = ABIVersion
		out = m
	case "handle":
		var in In
		if err := json.NewDecoder(stdin).Decode(&in); err != nil {
			fmt.Fprintln(os.Stderr, "hook: input:", err)
			return 1
		}
		out = handle(in)
	default:
		fmt.Fprintf(os.Stderr, "hook: unknown command %q; want manifest or handle\n", command)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "hook: output:", err)
		return 1
	}
	return 0
}

// ValidHMACSHA256 reports whether signature is the hex HMAC-SHA256 of body
// under secret, comparing in constant time. An empty secret never matches.
func ValidHMACSHA256(secret string, body []byte, signature string) bool {
	if secret == "" {
		return false
	}
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

// Equal compares two secrets in constant time. An empty one never matches.
func Equal(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
