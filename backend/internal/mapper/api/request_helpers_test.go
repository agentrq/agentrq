// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// at is the one instant the mapper tests use, so a whole view can be compared.
var at = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

// b62 is an id as the API writes it.
func b62(id int64) string { return monoflake.ID(id).String() }

// jsonRequest is a request for target, sent exactly as written so a test can
// put a malformed escape in the path, carrying body as JSON.
func jsonRequest(target, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.RequestURI = target // what the request line carries, unparsed
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return req
}

// mapRequest runs fn in a Fiber handler mounted on route and returns what it
// mapped req to. The app is immutable, so what fn read from the path and the
// query is still there once the request is over.
func mapRequest[T any](t *testing.T, route string, req *http.Request, fn func(*fiber.Ctx) T) T {
	t.Helper()
	app := fiber.New(fiber.Config{Immutable: true})
	var got T
	ran := false
	app.All(route, func(c *fiber.Ctx) error {
		got, ran = fn(c), true
		return nil
	})
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request %s: %v", req.RequestURI, err)
	}
	resp.Body.Close()
	if !ran {
		t.Fatalf("route %s did not match %s (status %d)", route, req.RequestURI, resp.StatusCode)
	}
	return got
}

// checkRequest maps each case and compares the result with want; a nil want
// is a request the mapper must refuse.
func checkRequest[T any](t *testing.T, route string, fn func(*fiber.Ctx) *T, cases []requestCase[T]) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapRequest(t, route, jsonRequest(tc.target, tc.body), fn)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

type requestCase[T any] struct {
	name, target, body string
	want               *T
}

// decodeResponse reads raw into a value of the view it was rendered from,
// after checking every key in it is camelCase.
func decodeResponse[T any](t *testing.T, raw []byte) T {
	t.Helper()
	assertCamelCase(t, raw)
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("response %s: %v", raw, err)
	}
	return v
}

// checkResponse decodes raw and compares it with want.
func checkResponse[T any](t *testing.T, raw []byte, want T) {
	t.Helper()
	if got := decodeResponse[T](t, raw); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v\nraw  %s", got, want, raw)
	}
}

// assertCamelCase fails on any object key in raw that is not camelCase: the
// API names no field with an underscore or a leading capital.
func assertCamelCase(t *testing.T, raw []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("response is not JSON: %s", raw)
	}
	for _, k := range nonCamelKeys("$", v) {
		t.Errorf("key %s is not camelCase in %s", k, raw)
	}
}

func nonCamelKeys(path string, v any) []string {
	var out []string
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if strings.ContainsAny(k, "_- ") || (k != "" && k[0] >= 'A' && k[0] <= 'Z') {
				out = append(out, path+"."+k)
			}
			out = append(out, nonCamelKeys(path+"."+k, child)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, nonCamelKeys(path+"[]", child)...)
		}
	}
	return out
}

func TestNonCamelKeys(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`{"ok":[{"workspace_id":"1","Name":"x","fine":{"alsoFine":1}}]}`), &v)
	got := nonCamelKeys("$", v)
	if len(got) != 2 {
		t.Errorf("got %v, want the snake_case and the capitalised key", got)
	}
}
