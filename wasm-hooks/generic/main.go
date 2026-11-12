// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

// Command generic is the built-in hook for any caller that can send a shared
// token in a header, such as a curl step in a CI. Its body becomes the event's
// payload unchanged.
package main

import (
	"encoding/json"

	"github.com/agentrq/agentrq/wasm-hooks/sdk"
)

var manifest = sdk.Manifest{
	Name:        "generic",
	Version:     "1.0.0",
	Description: "Any caller with a shared token. Passes the body through.",
	Inputs: []sdk.Input{
		{Name: "token", Label: "Token", Type: "string", Secret: true, Required: true},
		{Name: "header", Label: "Header carrying the token (default Authorization, also as Bearer)", Type: "string"},
	},
	Events: []sdk.Event{{Name: "request.received", Description: "A request with the right token arrived"}},
}

func main() { sdk.Main(manifest, handle) }

func handle(in sdk.In) sdk.Result {
	header := in.String("header")
	if header == "" {
		header = "Authorization"
	}
	token, got := in.String("token"), in.Request.Header(header)
	if token == "" || (!sdk.Equal(token, got) && !sdk.Equal("Bearer "+token, got)) {
		return sdk.Reject("missing or wrong token in " + header)
	}
	var payload any = string(in.Request.Body)
	if json.Valid(in.Request.Body) {
		payload = json.RawMessage(in.Request.Body)
	}
	dedup := in.Request.Header("Idempotency-Key")
	if dedup == "" {
		dedup = in.Request.Header("X-Request-Id")
	}
	return sdk.Accept(sdk.Out{Name: "request.received", DedupKey: dedup, Payload: payload})
}
