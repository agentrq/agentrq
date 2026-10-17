// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"testing"
)

// permissionRequest is the one channel notification still routed here, and
// retiredModels one acp-gateway sent over MCP before it spoke only native ACP.
const (
	permissionRequest = "notifications/claude/channel/permission_request"
	retiredModels     = "notifications/claude/channel/models"
)

// The bug this file guards against: the router used to decide by scanning the
// whole request body for one of these names, so a request that merely talked
// about them was intercepted, answered with an empty 200 and never executed.
// Two agent replies explaining the models rail were lost that way before anyone
// worked out why.

func TestCustomNotificationMethod_RoutesEachRealNotification(t *testing.T) {
	for _, method := range customNotificationMethods {
		t.Run(method, func(t *testing.T) {
			body := fmt.Appendf(nil, `{"jsonrpc":"2.0","method":%q,"params":{"task_id":"0iOq7fDWLPl"}}`, method)

			got, ok := customNotificationMethod(body)
			if !ok {
				t.Fatalf("a genuine %s notification was not recognised", method)
			}
			if got != method {
				t.Errorf("routed as %q, want %q", got, method)
			}
		})
	}
}

func TestCustomNotificationMethod_IgnoresTheNameInsideAToolCall(t *testing.T) {
	// The reduced form of the real failure: an agent telling its human how the
	// models notification works. The name appears in an argument, so a
	// substring match intercepted the call and the reply vanished.
	for _, method := range customNotificationMethods {
		t.Run(method, func(t *testing.T) {
			args, err := json.Marshal(map[string]any{
				"chatId": "0iOq7fDWLPl",
				"text":   "The gateway sends " + method + " whenever the agent's session config changes.",
			})
			if err != nil {
				t.Fatalf("marshalling the arguments: %v", err)
			}
			body := fmt.Appendf(nil,
				`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"reply","arguments":%s}}`, args)

			if got, ok := customNotificationMethod(body); ok {
				t.Fatalf("a tools/call mentioning %s was intercepted as %q; the caller loses the request", method, got)
			}
		})
	}
}

func TestCustomNotificationMethod_RoutesByTheMethodItDeclares(t *testing.T) {
	// A real notification whose payload happens to name a different one. It
	// belongs to the method it declares, not to whichever name appears in it.
	body := fmt.Appendf(nil,
		`{"jsonrpc":"2.0","method":%q,"params":{"note":%q}}`, permissionRequest, retiredModels)

	got, ok := customNotificationMethod(body)
	if !ok {
		t.Fatal("a genuine permission_request notification was not recognised")
	}
	if got != permissionRequest {
		t.Errorf("routed as %q, want the declared %q", got, permissionRequest)
	}
}

func TestCustomNotificationMethod_LeavesTheGatewaysOldMethodsToTheSDK(t *testing.T) {
	// acp-gateway reports these over native ACP v1 now. Over MCP they are
	// unknown methods, and the SDK says so.
	for _, method := range []string{
		"notifications/claude/channel/telemetry",
		"notifications/claude/channel/models",
		"notifications/claude/channel/commands",
		"notifications/claude/channel/agent",
		"notifications/claude/channel/concurrency",
	} {
		body := fmt.Appendf(nil, `{"jsonrpc":"2.0","method":%q,"params":{}}`, method)
		if got, ok := customNotificationMethod(body); ok {
			t.Errorf("%s was intercepted as %q; it should reach the SDK handler", method, got)
		}
	}
}

func TestCustomNotificationMethod_FallsThroughToTheSDK(t *testing.T) {
	// Everything here must reach the SDK handler. It answered these before the
	// interception existed and it is what reports what is wrong with them;
	// swallowing one with an empty 200 would turn a bad request into a lost
	// one.
	cases := []struct {
		name string
		body string
	}{
		{"an ordinary tool call", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"getWorkspace"}}`},
		{"a method the SDK owns", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`},
		{"an unrelated notification", `{"jsonrpc":"2.0","method":"notifications/initialized"}`},
		{"malformed JSON", `{"jsonrpc":"2.0","method":`},
		{"a batch array", `[{"jsonrpc":"2.0","method":"` + permissionRequest + `"}]`},
		{"an empty body", ``},
		{"JSON that is not an object", `"` + permissionRequest + `"`},
		{"a method that only starts the same way", `{"jsonrpc":"2.0","method":"` + permissionRequest + `/extra"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := customNotificationMethod([]byte(tc.body)); ok {
				t.Errorf("intercepted as %q; it should reach the SDK handler", got)
			}
		})
	}
}
