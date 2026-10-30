// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package pubsub

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	entityerr "github.com/agentrq/agentrq/backend/internal/data/entity/err"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeConfig hands Populate the pubsub section, or fails it.
type fakeConfig struct {
	timeout time.Duration
	err     error
}

func (f fakeConfig) Populate(key string, cfg any) error {
	if f.err != nil {
		return f.err
	}
	if key != _cfgKey {
		return errors.New("unexpected config key " + key)
	}
	cfg.(*pubsubConfig).MaxDurationForSubscriberToReceive = f.timeout
	return nil
}
func (fakeConfig) Env() string          { return "test" }
func (fakeConfig) App() string          { return "agentrq" }
func (fakeConfig) AppShortName() string { return "arq" }
func (fakeConfig) Version() string      { return "0.0.0" }

// counterIDs hands out 1000, 1001, ...
type counterIDs struct{ n atomic.Int64 }

func (c *counterIDs) NextID() int64 { return 1000 + c.n.Add(1) - 1 }

func newService(t *testing.T, timeout time.Duration) *service {
	t.Helper()
	s, err := New(Params{Config: fakeConfig{timeout: timeout}, IDGen: &counterIDs{}})
	require.NoError(t, err)
	return s.(*service)
}

// waitPublishersDone waits until no publish fan-out goroutine is left, which is
// the only way to know a send was given up rather than still pending.
func waitPublishersDone(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "pubsub.(*service).publish.func") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("publish goroutines still running after 5s:\n%s", buf[:n])
		}
		runtime.Gosched()
	}
}

func errCode(t *testing.T, err error) int {
	t.Helper()
	var e entityerr.Error
	require.True(t, errors.As(err, &e), "want an entity error, got %T: %v", err, err)
	return int(e.Code)
}

func TestNew(t *testing.T) {
	t.Run("a config that cannot be read is an error", func(t *testing.T) {
		s, err := New(Params{Config: fakeConfig{err: errors.New("no pubsub section")}, IDGen: &counterIDs{}})
		assert.Error(t, err)
		assert.Nil(t, s)
	})

	t.Run("keeps the subscriber timeout from config", func(t *testing.T) {
		s := newService(t, 7*time.Second)
		assert.Equal(t, 7*time.Second, s.cfg.MaxDurationForSubscriberToReceive)
	})
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	s := newService(t, time.Second)

	t.Run("a fixed id is created once and then reused", func(t *testing.T) {
		res, err := s.Create(ctx, CreatePubSubRequest{ID: 3})
		require.NoError(t, err)
		assert.Equal(t, int64(3), res.ID)

		sub, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 3})
		require.NoError(t, err)

		// Creating it again must not replace the topic and drop its subscribers.
		res, err = s.Create(ctx, CreatePubSubRequest{ID: 3})
		require.NoError(t, err)
		assert.Equal(t, int64(3), res.ID)
		ps, _ := s.pubsubs.Load(int64(3))
		assert.Len(t, ps.(*pubsub).subscribers, 1)
		assert.Equal(t, sub.ID, ps.(*pubsub).subscribers[0].id)
	})

	t.Run("no id takes one from the generator", func(t *testing.T) {
		res, err := s.Create(ctx, CreatePubSubRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, res.ID, int64(1000))
		_, ok := s.pubsubs.Load(res.ID)
		assert.True(t, ok, "the generated topic should be stored")
	})
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	s := newService(t, time.Second)

	t.Run("an unknown topic is a no-op", func(t *testing.T) {
		assert.NoError(t, s.Delete(ctx, DeletePubSubRequest{ID: 404}))
	})

	t.Run("a malformed topic is a 500", func(t *testing.T) {
		s.pubsubs.Store(int64(9), "not a pubsub")
		err := s.Delete(ctx, DeletePubSubRequest{ID: 9})
		assert.Equal(t, 500, errCode(t, err))
	})

	t.Run("closes every subscriber and forgets the topic", func(t *testing.T) {
		_, err := s.Create(ctx, CreatePubSubRequest{ID: 1})
		require.NoError(t, err)
		a, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)
		b, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)

		require.NoError(t, s.Delete(ctx, DeletePubSubRequest{ID: 1}))

		for _, ch := range []chan any{a.Events, b.Events} {
			select {
			case _, ok := <-ch:
				assert.False(t, ok, "a deleted topic's subscriber channel should be closed")
			case <-time.After(5 * time.Second):
				t.Fatal("subscriber channel was not closed")
			}
		}
		_, ok := s.pubsubs.Load(int64(1))
		assert.False(t, ok)
	})
}

func TestSubscribe(t *testing.T) {
	ctx := context.Background()
	s := newService(t, time.Second)

	t.Run("an unknown topic is a 404", func(t *testing.T) {
		_, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 404})
		assert.Equal(t, 404, errCode(t, err))
	})

	t.Run("a malformed topic is a 500", func(t *testing.T) {
		s.pubsubs.Store(int64(9), "not a pubsub")
		_, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 9})
		assert.Equal(t, 500, errCode(t, err))

		s.pubsubs.Store(int64(10), (*pubsub)(nil))
		_, err = s.Subscribe(ctx, SubscribeRequest{PubSubID: 10})
		assert.Equal(t, 500, errCode(t, err))
	})
}

func TestUnsubscribe(t *testing.T) {
	ctx := context.Background()
	s := newService(t, time.Second)

	t.Run("an unknown topic is a 404", func(t *testing.T) {
		err := s.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 404, ID: 1})
		assert.Equal(t, 404, errCode(t, err))
	})

	t.Run("a malformed topic is a 500", func(t *testing.T) {
		s.pubsubs.Store(int64(9), "not a pubsub")
		err := s.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 9, ID: 1})
		assert.Equal(t, 500, errCode(t, err))

		s.pubsubs.Store(int64(10), (*pubsub)(nil))
		err = s.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 10, ID: 1})
		assert.Equal(t, 500, errCode(t, err))
	})

	t.Run("removes only the named subscriber and closes its channel", func(t *testing.T) {
		_, err := s.Create(ctx, CreatePubSubRequest{ID: 1})
		require.NoError(t, err)
		a, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)
		b, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)
		c, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)

		// An id nobody holds changes nothing.
		require.NoError(t, s.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 1, ID: -1}))
		ps, _ := s.pubsubs.Load(int64(1))
		assert.Len(t, ps.(*pubsub).subscribers, 3)

		require.NoError(t, s.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 1, ID: a.ID}))
		select {
		case _, ok := <-a.Events:
			assert.False(t, ok, "the unsubscribed channel should be closed")
		case <-time.After(5 * time.Second):
			t.Fatal("unsubscribed channel was not closed")
		}

		var left []int64
		for _, sub := range ps.(*pubsub).subscribers {
			left = append(left, sub.id)
		}
		assert.ElementsMatch(t, []int64{b.ID, c.ID}, left)
	})
}

func TestPublish(t *testing.T) {
	ctx := context.Background()

	t.Run("an unknown topic is a 404", func(t *testing.T) {
		s := newService(t, time.Second)
		res, err := s.Publish(ctx, PublishRequest{PubSubID: 404, Event: "x"})
		assert.Nil(t, res)
		assert.Equal(t, 404, errCode(t, err))
	})

	t.Run("a malformed topic is a 500", func(t *testing.T) {
		s := newService(t, time.Second)
		s.pubsubs.Store(int64(9), "not a pubsub")
		res, err := s.Publish(ctx, PublishRequest{PubSubID: 9, Event: "x"})
		assert.Nil(t, res)
		assert.Equal(t, 500, errCode(t, err))
	})

	t.Run("every subscriber receives the event", func(t *testing.T) {
		s := newService(t, 5*time.Second)
		_, err := s.Create(ctx, CreatePubSubRequest{ID: 1})
		require.NoError(t, err)
		a, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)
		b, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)

		res, err := s.Publish(ctx, PublishRequest{PubSubID: 1, Event: "hello"})
		require.NoError(t, err)
		assert.NotZero(t, res.ID)

		var wg sync.WaitGroup
		for _, ch := range []chan any{a.Events, b.Events} {
			wg.Add(1)
			go func(ch chan any) {
				defer wg.Done()
				select {
				case got := <-ch:
					assert.Equal(t, "hello", got)
				case <-time.After(5 * time.Second):
					t.Error("subscriber did not receive the event")
				}
			}(ch)
		}
		wg.Wait()
		waitPublishersDone(t)
	})

	t.Run("a subscriber that never reads is given up after the timeout", func(t *testing.T) {
		s := newService(t, time.Millisecond)
		_, err := s.Create(ctx, CreatePubSubRequest{ID: 1})
		require.NoError(t, err)
		slow, err := s.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
		require.NoError(t, err)

		_, err = s.Publish(ctx, PublishRequest{PubSubID: 1, Event: "dropped"})
		require.NoError(t, err)
		waitPublishersDone(t)

		select {
		case got := <-slow.Events:
			t.Fatalf("the event should have been dropped, got %v", got)
		default:
		}
	})

	t.Run("a send on a closed subscriber channel is recovered", func(t *testing.T) {
		s := newService(t, 5*time.Second)
		closed := make(chan any)
		close(closed)
		s.pubsubs.Store(int64(1), &pubsub{
			id: 1,
			subscribers: []subscriber{{
				id:      7,
				channel: closed,
				ctx:     context.Background(),
				cancel:  func() {},
			}},
		})

		_, err := s.Publish(ctx, PublishRequest{PubSubID: 1, Event: "x"})
		require.NoError(t, err)
		// Reaching the end without the test binary panicking is the assertion.
		waitPublishersDone(t)
	})
}

func TestPublishWithTimeout(t *testing.T) {
	t.Run("delivers when the subscriber reads", func(t *testing.T) {
		ch := make(chan any, 1)
		require.NoError(t, publishWithTimeout(context.Background(), context.Background(), ch, 1))
		assert.Equal(t, 1, <-ch)
	})

	t.Run("gives up when the publisher's deadline passes", func(t *testing.T) {
		opCtx, cancel := context.WithCancel(context.Background())
		cancel()
		err := publishWithTimeout(opCtx, context.Background(), make(chan any), 1)
		assert.ErrorIs(t, err, context.Canceled)
		assert.NotContains(t, err.Error(), "subscriber canceled")
	})

	t.Run("gives up when the subscriber goes away", func(t *testing.T) {
		subCtx, cancel := context.WithCancel(context.Background())
		cancel()
		err := publishWithTimeout(context.Background(), subCtx, make(chan any), 1)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Contains(t, err.Error(), "subscriber canceled")
	})
}
