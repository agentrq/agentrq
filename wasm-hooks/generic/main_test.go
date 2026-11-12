// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"

	"github.com/agentrq/agentrq/wasm-hooks/sdk"
)

func request(inputs map[string]string, headers map[string]string, body string) sdk.In {
	in := sdk.In{Inputs: map[string]json.RawMessage{}, Request: sdk.Request{Headers: map[string][]string{}, Body: []byte(body)}}
	for k, v := range inputs {
		in.Inputs[k], _ = json.Marshal(v)
	}
	for k, v := range headers {
		in.Request.Headers[k] = []string{v}
	}
	return in
}

func TestTheTokenIsChecked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		inputs  map[string]string
		headers map[string]string
		accept  bool
	}{
		{"as is", map[string]string{"token": "t0k"}, map[string]string{"authorization": "t0k"}, true},
		{"as a bearer", map[string]string{"token": "t0k"}, map[string]string{"authorization": "Bearer t0k"}, true},
		{"in its own header", map[string]string{"token": "t0k", "header": "X-Token"}, map[string]string{"x-token": "t0k"}, true},
		{"wrong", map[string]string{"token": "t0k"}, map[string]string{"authorization": "Bearer nope"}, false},
		{"missing", map[string]string{"token": "t0k"}, nil, false},
		{"in another header", map[string]string{"token": "t0k", "header": "X-Token"}, map[string]string{"authorization": "t0k"}, false},
		{"no token set", nil, map[string]string{"authorization": "Bearer "}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if res := handle(request(tc.inputs, tc.headers, "")); res.Accept != tc.accept {
				t.Errorf("accept = %v, want %v (%s)", res.Accept, tc.accept, res.Reason)
			}
		})
	}
}

func TestTheBodyIsThePayload(t *testing.T) {
	auth := map[string]string{"token": "t"}
	res := handle(request(auth, map[string]string{"authorization": "t", "idempotency-key": "k1"}, `{"a":1}`))
	b, _ := json.Marshal(res.Events[0])
	if want := `{"name":"request.received","dedupKey":"k1","payload":{"a":1}}`; string(b) != want {
		t.Errorf("event = %s, want %s", b, want)
	}
	res = handle(request(auth, map[string]string{"authorization": "t", "x-request-id": "r1"}, "plain text"))
	if e := res.Events[0]; e.Payload != "plain text" || e.DedupKey != "r1" {
		t.Errorf("event = %+v", e)
	}
	if manifest.Events[0].Name != res.Events[0].Name {
		t.Error("the event is not the one the manifest declares")
	}
}
