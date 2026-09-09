package mcp

import (
	"encoding/json"
	"fmt"
	"testing"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
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
	// belongs to the method it declares, not to whichever name appears first
	// in the allowlist.
	body := fmt.Appendf(nil,
		`{"jsonrpc":"2.0","method":%q,"params":{"note":%q}}`,
		mcpctrl.AgentTelemetryNotificationMethod, mcpctrl.AgentModelsNotificationMethod)

	got, ok := customNotificationMethod(body)
	if !ok {
		t.Fatal("a genuine telemetry notification was not recognised")
	}
	if got != mcpctrl.AgentTelemetryNotificationMethod {
		t.Errorf("routed as %q, want the declared %q", got, mcpctrl.AgentTelemetryNotificationMethod)
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
		{"a batch array", `[{"jsonrpc":"2.0","method":"` + mcpctrl.AgentModelsNotificationMethod + `"}]`},
		{"an empty body", ``},
		{"JSON that is not an object", `"` + mcpctrl.AgentModelsNotificationMethod + `"`},
		{"a method that only starts the same way", `{"jsonrpc":"2.0","method":"` + mcpctrl.AgentModelsNotificationMethod + `/extra"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := customNotificationMethod([]byte(tc.body)); ok {
				t.Errorf("intercepted as %q; it should reach the SDK handler", got)
			}
		})
	}
}
