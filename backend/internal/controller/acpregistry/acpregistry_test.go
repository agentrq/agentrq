// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

const sampleRegistry = `{
  "version": "1.0.0",
  "agents": [
    {"id": "gemini", "name": "Gemini CLI", "version": "0.9.0", "description": "Google's agent",
     "distribution": {"npx": {"package": "@google/gemini-cli"}}},
    {"id": "amp-acp", "name": "Amp", "version": "0.10.0", "description": "ACP wrapper for Amp",
     "distribution": {"npx": {}, "binary": {"linux-x86_64": {}}}},
    {"id": "", "name": "No id"},
    {"id": "bare", "name": " "}
  ]
}`

// encodeIDs answers the agents' ids, so a test can read what was kept.
func encodeIDs(agents []entity.AcpRegistryAgent) []byte {
	body, _ := json.Marshal(agents)
	return body
}

// fakeClock is a now the test moves by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// registryServer answers each request with the next handler's reply, and the
// last one from then on, counting the requests.
type registryServer struct {
	*httptest.Server
	hits    atomic.Int32
	mu      sync.Mutex
	replies []func(http.ResponseWriter)
}

func newRegistryServer(t *testing.T, replies ...func(http.ResponseWriter)) *registryServer {
	t.Helper()
	s := &registryServer{replies: replies}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(s.hits.Add(1)) - 1
		s.mu.Lock()
		reply := s.replies[min(n, len(s.replies)-1)]
		s.mu.Unlock()
		reply(w)
	}))
	t.Cleanup(s.Close)
	return s
}

func answer(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = w.Write([]byte(body)) }
}

func status(code int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func newCatalogue(url string, clock *fakeClock) *Catalogue {
	c := New(url, http.DefaultClient, encodeIDs)
	c.now = clock.now
	return c
}

func decode(t *testing.T, body []byte) []entity.AcpRegistryAgent {
	t.Helper()
	var agents []entity.AcpRegistryAgent
	if err := json.Unmarshal(body, &agents); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return agents
}

// waitIdle waits for a refresh started behind a stale answer to finish.
func waitIdle(t *testing.T, c *Catalogue) {
	t.Helper()
	c.mu.RLock()
	wait := c.inflight
	c.mu.RUnlock()
	if wait == nil {
		return
	}
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never finished")
	}
}

func TestLoadAnswersTheRegistrysAgentsSortedByName(t *testing.T) {
	srv := newRegistryServer(t, answer(sampleRegistry))
	c := newCatalogue(srv.URL, &fakeClock{t: time.Unix(0, 0)})

	body, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := decode(t, body)
	want := []entity.AcpRegistryAgent{
		{ID: "amp-acp", Name: "Amp", Version: "0.10.0", Description: "ACP wrapper for Amp", Runtimes: []string{"binary", "npx"}},
		{ID: "bare", Name: "bare", Runtimes: []string{}},
		{ID: "gemini", Name: "Gemini CLI", Version: "0.9.0", Description: "Google's agent", Runtimes: []string{"npx"}},
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("agents = %s, want %s", gotJSON, wantJSON)
	}
}

func TestLoadFetchesOnceWithinTheTTL(t *testing.T) {
	srv := newRegistryServer(t, answer(sampleRegistry))
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newCatalogue(srv.URL, clock)

	for range 3 {
		if _, err := c.Load(context.Background()); err != nil {
			t.Fatalf("Load: %v", err)
		}
		clock.advance(TTL / 4)
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("fetched %d times within the TTL, want 1", n)
	}
}

func TestConcurrentFirstLoadsShareOneFetch(t *testing.T) {
	release := make(chan struct{})
	srv := newRegistryServer(t, func(w http.ResponseWriter) {
		<-release
		_, _ = w.Write([]byte(sampleRegistry))
	})
	c := newCatalogue(srv.URL, &fakeClock{t: time.Unix(0, 0)})

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Load(context.Background())
			errs <- err
		}()
	}
	for {
		c.mu.RLock()
		started := c.inflight != nil
		c.mu.RUnlock()
		if started && srv.hits.Load() == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("fetched %d times for concurrent loads, want 1", n)
	}
}

func TestAnExpiredAnswerIsServedWhileOneRefreshRuns(t *testing.T) {
	updated := strings.Replace(sampleRegistry, `"Amp"`, `"Amp Code"`, 1)
	release := make(chan struct{})
	srv := newRegistryServer(t, answer(sampleRegistry), func(w http.ResponseWriter) {
		<-release
		_, _ = w.Write([]byte(updated))
	})
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newCatalogue(srv.URL, clock)
	first, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	clock.advance(TTL)
	for range 3 {
		stale, err := c.Load(context.Background())
		if err != nil || string(stale) != string(first) {
			t.Fatalf("expired Load = %s, %v; want the previous answer", stale, err)
		}
	}
	close(release)
	waitIdle(t, c)

	fresh, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := decode(t, fresh)[0].Name; got != "Amp Code" {
		t.Fatalf("after the refresh, first agent = %q, want %q", got, "Amp Code")
	}
	if n := srv.hits.Load(); n != 2 {
		t.Fatalf("fetched %d times, want 2 (one refresh for three expired loads)", n)
	}
}

func TestAFailedFirstFetchIsHeldOffThenRetried(t *testing.T) {
	srv := newRegistryServer(t, status(http.StatusBadGateway), answer(sampleRegistry))
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newCatalogue(srv.URL, clock)

	if _, err := c.Load(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Load after a failed fetch = %v, want ErrUnavailable", err)
	}
	clock.advance(RetryAfter - time.Second)
	if _, err := c.Load(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Load while held off = %v, want ErrUnavailable", err)
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("fetched %d times while held off, want 1", n)
	}

	clock.advance(time.Second)
	if _, err := c.Load(context.Background()); err != nil {
		t.Fatalf("Load after RetryAfter: %v", err)
	}
}

func TestAFailedRefreshKeepsThePreviousAnswer(t *testing.T) {
	srv := newRegistryServer(t, answer(sampleRegistry), status(http.StatusInternalServerError))
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newCatalogue(srv.URL, clock)
	first, _ := c.Load(context.Background())

	clock.advance(TTL)
	if _, err := c.Load(context.Background()); err != nil {
		t.Fatalf("expired Load: %v", err)
	}
	waitIdle(t, c)

	again, err := c.Load(context.Background())
	if err != nil || string(again) != string(first) {
		t.Fatalf("Load after a failed refresh = %s, %v; want the previous answer", again, err)
	}
	if n := srv.hits.Load(); n != 2 {
		t.Fatalf("fetched %d times, want 2: the failure holds off the next refresh", n)
	}
}

func TestALoadStopsWaitingWhenItsContextEnds(t *testing.T) {
	release := make(chan struct{})
	srv := newRegistryServer(t, func(w http.ResponseWriter) {
		<-release
		_, _ = w.Write([]byte(sampleRegistry))
	})
	defer close(release)
	c := newCatalogue(srv.URL, &fakeClock{t: time.Unix(0, 0)})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Load(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load = %v, want its own deadline", err)
	}
}

func TestALoadReadsAnAnswerStoredWhileItTookTheLock(t *testing.T) {
	srv := newRegistryServer(t, answer(sampleRegistry))
	clock := &fakeClock{t: time.Unix(0, 0)}
	c := newCatalogue(srv.URL, clock)
	first, _ := c.Load(context.Background())

	// The first look at the clock finds it expired, the second, under the
	// write lock, finds it fresh: what a refresh landing in between does.
	calls := 0
	c.now = func() time.Time {
		calls++
		if calls == 1 {
			return clock.now().Add(TTL)
		}
		return clock.now()
	}
	body, err := c.Load(context.Background())
	if err != nil || string(body) != string(first) {
		t.Fatalf("Load = %s, %v; want the stored answer", body, err)
	}
	if n := srv.hits.Load(); n != 1 {
		t.Fatalf("fetched %d times, want 1", n)
	}
}

func TestFetchRefusesWhatIsNotARegistry(t *testing.T) {
	tooLarge := `{"agents":[{"id":"x","description":"` + strings.Repeat("a", maxBody) + `"}]}`
	cases := []struct {
		name  string
		reply func(http.ResponseWriter)
		want  string
	}{
		{"an error status", status(http.StatusNotFound), "404"},
		{"a body over the limit", answer(tooLarge), "larger than"},
		{"malformed JSON", answer(`{"agents":`), "decode registry"},
		{"no agents", answer(`{"agents":[]}`), "no agents"},
		{"only agents without an id", answer(`{"agents":[{"name":"x"}]}`), "no agents"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRegistryServer(t, tc.reply)
			c := newCatalogue(srv.URL, &fakeClock{t: time.Unix(0, 0)})
			_, err := c.fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("fetch = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestFetchReportsARequestThatCannotBeMadeOrSent(t *testing.T) {
	c := newCatalogue("://not a url", &fakeClock{t: time.Unix(0, 0)})
	if _, err := c.fetch(context.Background()); err == nil {
		t.Fatal("fetch of a malformed URL succeeded")
	}

	srv := newRegistryServer(t, answer(sampleRegistry))
	srv.Close()
	c = newCatalogue(srv.URL, &fakeClock{t: time.Unix(0, 0)})
	if _, err := c.fetch(context.Background()); err == nil {
		t.Fatal("fetch from a closed server succeeded")
	}
}

func TestFetchReportsABodyCutShort(t *testing.T) {
	srv := newRegistryServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`{"agents":`))
	})
	c := newCatalogue(srv.URL, &fakeClock{t: time.Unix(0, 0)})
	if _, err := c.fetch(context.Background()); err == nil {
		t.Fatal("fetch of a truncated body succeeded")
	}
}

func TestNewUsesTheRealClockAndTimings(t *testing.T) {
	c := New(URL, http.DefaultClient, encodeIDs)
	if c.ttl != TTL || c.retry != RetryAfter || c.now == nil {
		t.Fatalf("New = ttl %v, retry %v; want %v, %v and a clock", c.ttl, c.retry, TTL, RetryAfter)
	}
}
