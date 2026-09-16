// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package enrol exchanges a one-time code for a machine token.
//
// Enrolment is the only moment a secret crosses a human's hands, and it is
// deliberately a local act: somebody reads a short code from the control panel
// and types it at a terminal on the machine being enrolled. There is no remote
// enrolment, and nothing the backend can say will cause a machine to enrol
// itself.
//
// A code rather than an OAuth2 browser flow, because the obvious place to run
// this daemon is a headless build box. A flow that needs a browser on the
// machine being enrolled is a flow that does not work where it is most wanted.
package enrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Path is the endpoint that trades a code for a token.
const Path = "/api/v1/machines/enroll"

// Errors a caller is expected to tell apart. A rejected code is something the
// person can fix by fetching a new one; the others are not.
var (
	ErrCodeRejected = errors.New("enrol: the enrolment code was rejected")
	ErrServer       = errors.New("enrol: the server could not complete enrolment")
	ErrMalformed    = errors.New("enrol: the server's answer could not be understood")
)

// Machine is what this daemon reports about the box it is running on.
//
// Everything here is chosen by the machine and none of it is a secret: it is
// what the control panel shows so a person can tell one machine from another.
type Machine struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
}

// Request is the enrolment body.
type Request struct {
	Code string `json:"code"`
	Machine
}

// Response is what the server returns.
type Response struct {
	MachineID    string `json:"machineId"`
	MachineToken string `json:"machineToken"`
}

// Doer is the HTTP client, narrowed to what this package uses so a test can
// supply one without a server.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// DefaultTimeout bounds an enrolment attempt.
//
// A person is stood at a terminal waiting for this, and the code they typed
// expires. Hanging on an unreachable server until some default timeout is worse
// than failing quickly enough that they can try again while the code is alive.
const DefaultTimeout = 30 * time.Second

// Enrol trades a one-time code for a machine identity.
//
// The code is trimmed and upper-cased before it is sent, because it is read off
// a screen and typed by a person — and "abcd-1234" failing when `ABCD-1234`
// would have worked is an error message nobody deserves.
func Enrol(ctx context.Context, client Doer, serverURL, code string, m Machine) (Response, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return Response{}, fmt.Errorf("%w: no code given", ErrCodeRejected)
	}

	body, err := json.Marshal(Request{Code: code, Machine: m})
	if err != nil {
		return Response{}, fmt.Errorf("enrol: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(serverURL, "/")+Path, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("enrol: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Sent from the first request onwards so a server can answer an old daemon
	// differently, and so we can find out what is actually deployed.
	req.Header.Set("X-AgentRQ-Version", m.Version)
	req.Header.Set("X-AgentRQ-Protocol", fmt.Sprint(wire.Version))

	resp, err := client.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("enrol: %s: %w", serverURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bounded, because an enrolment response is a few hundred bytes and an
	// unbounded read from a server we do not yet trust is an easy way to be
	// handed a gigabyte.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return Response{}, fmt.Errorf("enrol: read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusCreated:
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return Response{}, fmt.Errorf("%w (%s): %s", ErrCodeRejected, resp.Status, serverMessage(payload))
	default:
		return Response{}, fmt.Errorf("%w (%s): %s", ErrServer, resp.Status, serverMessage(payload))
	}

	var out Response
	if err := json.Unmarshal(payload, &out); err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if out.MachineID == "" || out.MachineToken == "" {
		return Response{}, fmt.Errorf("%w: the response carried no machine id or token", ErrMalformed)
	}
	return out, nil
}

// serverMessage pulls a human-readable reason out of an error body.
//
// Falls back to the raw text rather than to nothing: a person debugging an
// enrolment needs whatever the server actually said, even when it is not the
// shape we hoped for.
func serverMessage(payload []byte) string {
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &body); err == nil {
		if body.Error != "" {
			return body.Error
		}
		if body.Message != "" {
			return body.Message
		}
	}
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return "no reason given"
	}
	const max = 200
	if len(text) > max {
		return text[:max] + "…"
	}
	return text
}
