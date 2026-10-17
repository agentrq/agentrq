// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func probe(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("%s Cache-Control = %q, want no-store", path, got)
	}
	return rr
}

func TestLivenessChecksNothingButTheProcess(t *testing.T) {
	pinged := false
	h := New(func(context.Context) error { pinged = true; return errors.New("database unavailable") })

	rr := probe(t, h, LivePath)
	if rr.Code != http.StatusOK || rr.Body.String() != "ok" {
		t.Errorf("liveness = %d %q, want 200 ok", rr.Code, rr.Body.String())
	}
	if pinged {
		t.Error("liveness pinged the database")
	}
}

func TestLivenessStaysUpWhileDraining(t *testing.T) {
	h := New(func(context.Context) error { return nil })
	h.Drain()

	if rr := probe(t, h, LivePath); rr.Code != http.StatusOK {
		t.Errorf("liveness while draining = %d, want 200", rr.Code)
	}
}

func TestReadinessPingsTheDatabaseWithADeadline(t *testing.T) {
	var deadline bool
	h := New(func(ctx context.Context) error {
		_, deadline = ctx.Deadline()
		return nil
	})

	rr := probe(t, h, ReadyPath)
	if rr.Code != http.StatusOK || rr.Body.String() != "ok" {
		t.Errorf("readiness = %d %q, want 200 ok", rr.Code, rr.Body.String())
	}
	if !deadline {
		t.Error("the ping had no deadline")
	}
}

func TestReadinessFailsWhenTheDatabaseIsUnavailable(t *testing.T) {
	h := New(func(context.Context) error { return errors.New("connection refused on 10.0.0.7:5432") })

	rr := probe(t, h, ReadyPath)
	if rr.Code != http.StatusServiceUnavailable || rr.Body.String() != "unavailable" {
		t.Errorf("readiness = %d %q, want 503 unavailable with no detail", rr.Code, rr.Body.String())
	}
}

func TestReadinessFailsOnceDrainingWithoutAPing(t *testing.T) {
	pinged := false
	h := New(func(context.Context) error { pinged = true; return nil })
	h.Drain()

	rr := probe(t, h, ReadyPath)
	if rr.Code != http.StatusServiceUnavailable || rr.Body.String() != "draining" {
		t.Errorf("readiness = %d %q, want 503 draining", rr.Code, rr.Body.String())
	}
	if pinged {
		t.Error("a draining instance still pinged the database")
	}
}
