// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

const (
	// MaxEventsPerRequest is the most events one request may become.
	MaxEventsPerRequest = 32
	maxDedupKeySize     = 255
	maxFAQs             = 20
	maxReasonSize       = 512
)

// Request is the raw request the hook validates. Header names are lowercase.
type Request struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   map[string][]string `json:"query"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

// HandleInput is what a hook reads on stdin for "handle".
type HandleInput struct {
	ABIVersion     int            `json:"abiVersion"`
	RegistrationID string         `json:"registrationId"`
	Request        Request        `json:"request"`
	Inputs         map[string]any `json:"inputs"`
	Now            time.Time      `json:"now"`
}

// FAQ is a question an agent may have about an event, answered up front.
type FAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}

// Result is a hook's answer to one request, checked.
type Result struct {
	// Accept is false when the request failed the hook's validation, such
	// as a wrong signature; Reason then says why.
	Accept bool
	Reason string
	Events []EventOut
}

// EventOut is one event the hook wrote. Payload is what the event carries,
// written by the hook, never the request's body as such.
type EventOut struct {
	Name     string
	DedupKey string
	Payload  string
	FAQ      []FAQ
}

type rawResult struct {
	Accept bool       `json:"accept"`
	Reason string     `json:"reason"`
	Events []rawEvent `json:"events"`
}

type rawEvent struct {
	Name     string          `json:"name"`
	DedupKey string          `json:"dedupKey"`
	Payload  json.RawMessage `json:"payload"`
	FAQ      []FAQ           `json:"faq"`
}

// OutputError is an answer the hook should not have given. The hook is
// deterministic, so running it again would give the same answer: the
// delivery fails without a retry.
type OutputError struct{ msg string }

func (e *OutputError) Error() string { return "hook output: " + e.msg }

func outputErr(format string, a ...any) error {
	return &OutputError{msg: fmt.Sprintf(format, a...)}
}

// parseResult reads a hook's answer to "handle" and checks it against what
// its manifest declares and the payload limit.
func parseResult(out []byte, m Manifest, maxPayloadSize int) (Result, error) {
	var raw rawResult
	if err := json.Unmarshal(out, &raw); err != nil {
		return Result{}, outputErr("not JSON: %v", err)
	}
	if len(raw.Reason) > maxReasonSize {
		return Result{}, outputErr("reason is over %d bytes", maxReasonSize)
	}
	if !raw.Accept {
		if len(raw.Events) > 0 {
			return Result{}, outputErr("a rejected request cannot have events")
		}
		return Result{Reason: raw.Reason}, nil
	}
	if len(raw.Events) > MaxEventsPerRequest {
		return Result{}, outputErr("%d events; at most %d", len(raw.Events), MaxEventsPerRequest)
	}
	res := Result{Accept: true, Reason: raw.Reason, Events: make([]EventOut, 0, len(raw.Events))}
	for i, e := range raw.Events {
		switch {
		case !m.Declares(e.Name):
			return Result{}, outputErr("event %d: %q is not declared in the manifest", i, e.Name)
		case len(e.DedupKey) > maxDedupKeySize:
			return Result{}, outputErr("event %d: dedupKey is over %d bytes", i, maxDedupKeySize)
		case len(e.FAQ) > maxFAQs:
			return Result{}, outputErr("event %d: more than %d faq entries", i, maxFAQs)
		}
		payload := payloadText(e.Payload)
		size := len(payload)
		for _, f := range e.FAQ {
			size += len(f.Q) + len(f.A)
		}
		if size > maxPayloadSize {
			return Result{}, outputErr("event %d: payload and faq are %d bytes; at most %d", i, size, maxPayloadSize)
		}
		res.Events = append(res.Events, EventOut{Name: e.Name, DedupKey: e.DedupKey, Payload: payload, FAQ: e.FAQ})
	}
	return res, nil
}

// payloadText is the payload as the event carries it: a JSON string as is,
// any other value compacted, nothing as empty. raw is a value the decoder
// already accepted, so neither step can fail.
func payloadText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var buf bytes.Buffer
	_ = json.Compact(&buf, raw)
	return buf.String()
}
