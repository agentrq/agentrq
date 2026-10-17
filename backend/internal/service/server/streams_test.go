// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Shutdown ends the event streams, which would otherwise hold the drain open,
// and leaves other requests to finish.
func TestShutdownEndsEventStreamsOnly(t *testing.T) {
	ended := make(chan string, 2)
	release := make(chan struct{})
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			ended <- r.URL.Path + " cancelled"
		case <-release:
			ended <- r.URL.Path + " finished"
		}
	})
	svc, _ := New(Params{Router: router})
	srv := httptest.NewServer(svc.(*service))
	defer srv.Close()

	open := func(path string) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Accept", "text/event-stream")
		if path == "/api" {
			req.Header.Set("Accept", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		go func() { io.Copy(io.Discard, res.Body); res.Body.Close() }()
	}
	open("/stream")
	open("/api")

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ended:
		if got != "/stream cancelled" {
			t.Fatalf("first to end: %s, want the stream", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event stream was not ended")
	}
	select {
	case got := <-ended:
		t.Fatalf("%s before it was released", got)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if got := <-ended; got != "/api finished" {
		t.Errorf("the request %s, want finished", got)
	}
}

func TestIsEventStream(t *testing.T) {
	for _, tt := range []struct {
		method, accept string
		want           bool
	}{
		{http.MethodGet, "text/event-stream", true},
		{http.MethodGet, "application/json, text/event-stream", true},
		{http.MethodGet, "application/json", false},
		// An MCP tool call may answer as a stream, but it is a request to finish.
		{http.MethodPost, "application/json, text/event-stream", false},
	} {
		r := httptest.NewRequest(tt.method, "/", nil)
		r.Header.Set("Accept", tt.accept)
		if got := isEventStream(r); got != tt.want {
			t.Errorf("%s Accept %q: %v, want %v", tt.method, tt.accept, got, tt.want)
		}
	}
}
