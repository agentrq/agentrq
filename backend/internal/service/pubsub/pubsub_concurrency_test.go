package pubsub

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeIDGen struct{ n int64 }

func (f *fakeIDGen) NextID() int64 { return atomic.AddInt64(&f.n, 1) }

func newTestPubSub() *service {
	return &service{
		cfg:   pubsubConfig{MaxDurationForSubscriberToReceive: 500 * time.Millisecond},
		idgen: &fakeIDGen{},
	}
}

func TestPubSub_Delivery(t *testing.T) {
	svc := newTestPubSub()
	ctx := context.Background()
	cr, _ := svc.Create(ctx, CreatePubSubRequest{ID: 1})
	sub, err := svc.Subscribe(ctx, SubscribeRequest{PubSubID: cr.ID})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := svc.Publish(ctx, PublishRequest{PubSubID: cr.ID, Event: "hello"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case e := <-sub.Events:
		if e != "hello" {
			t.Fatalf("got %v, want hello", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delivery within timeout")
	}
}

func TestPubSub_UnsubscribeStopsDelivery(t *testing.T) {
	svc := newTestPubSub()
	ctx := context.Background()
	svc.Create(ctx, CreatePubSubRequest{ID: 1})
	subA, _ := svc.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
	subB, _ := svc.Subscribe(ctx, SubscribeRequest{PubSubID: 1})

	if err := svc.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 1, ID: subA.ID}); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	// Drain B so the fan-out can complete.
	bGot := make(chan any, 1)
	go func() { bGot <- <-subB.Events }()

	if _, err := svc.Publish(ctx, PublishRequest{PubSubID: 1, Event: "x"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case v := <-bGot:
		if v != "x" {
			t.Fatalf("B got %v, want x", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("remaining subscriber B did not receive")
	}

	// A was unsubscribed: it must not receive a value (its channel is closed).
	select {
	case _, ok := <-subA.Events:
		if ok {
			t.Fatal("unsubscribed subscriber A received a value")
		}
	case <-time.After(time.Second):
		// No value delivered is also acceptable.
	}
}

// Previously, publish sent on subscriber channels after releasing the lock, racing
// Unsubscribe/Delete which close them (a send-on-closed panic, only masked by recover).
// This churns publishes against subscribe/unsubscribe and must complete with no panic
// and no deadlock.
func TestPubSub_ConcurrentPublishUnsubscribe(t *testing.T) {
	svc := newTestPubSub()
	ctx := context.Background()
	svc.Create(ctx, CreatePubSubRequest{ID: 1})

	// A stable subscriber that always drains.
	stable, _ := svc.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-stable.Events:
			}
		}
	}()

	var wg sync.WaitGroup
	for p := 0; p < 8; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				svc.Publish(ctx, PublishRequest{PubSubID: 1, Event: i})
			}
		}()
	}
	for c := 0; c < 8; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s, err := svc.Subscribe(ctx, SubscribeRequest{PubSubID: 1})
				if err != nil {
					continue
				}
				// Drain until the channel is closed by Unsubscribe.
				go func(ch chan any) {
					for range ch {
					}
				}(s.Events)
				svc.Unsubscribe(ctx, UnsubscribeRequest{PubSubID: 1, ID: s.ID})
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deadlock: concurrent publish/unsubscribe did not finish")
	}
	close(stop)
}
