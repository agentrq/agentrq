package pubsub

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/entity/err"
	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	zlog "github.com/rs/zerolog/log"
)

type (
	Service interface {
		Create(ctx context.Context, req CreatePubSubRequest) (*CreatePubSubResponse, error)
		Delete(ctx context.Context, req DeletePubSubRequest) error
		Publish(ctx context.Context, req PublishRequest) (*PublishResponse, error)
		Subscribe(ctx context.Context, req SubscribeRequest) (*SubscribeResponse, error)
		Unsubscribe(ctx context.Context, req UnsubscribeRequest) error
	}

	service struct {
		cfg     pubsubConfig
		idgen   idgen.Service
		pubsubs sync.Map
	}

	Params struct {
		Config config.Service
		IDGen  idgen.Service
	}

	pubsub struct {
		id          int64
		subscribers []subscriber
		mutex       sync.RWMutex
	}

	subscriber struct {
		id      int64
		channel chan any
		ctx     context.Context
		cancel  context.CancelFunc
	}

	pubsubConfig struct {
		MaxDurationForSubscriberToReceive time.Duration `yaml:"maxDurationForSubscriberToReceive"`
	}
)

const (
	_cfgKey = "pubsub"

	_logPrefix = "[pubsub] "
)

func New(p Params) (Service, error) {
	var cfg pubsubConfig
	err := p.Config.Populate(_cfgKey, &cfg)
	if err != nil {
		return nil, err
	}

	c := &service{
		cfg:     cfg,
		idgen:   p.IDGen,
		pubsubs: sync.Map{},
	}
	return c, nil
}

func (c *service) Create(ctx context.Context, req CreatePubSubRequest) (*CreatePubSubResponse, error) {
	id := req.ID
	if id > 0 {
		_, ok := c.pubsubs.Load(id)
		if ok {
			return &CreatePubSubResponse{
				ID: id,
			}, nil
		}
	} else {
		id = c.idgen.NextID()
	}

	c.pubsubs.Store(id, &pubsub{
		id:          id,
		subscribers: make([]subscriber, 0, 1),
		mutex:       sync.RWMutex{},
	})

	return &CreatePubSubResponse{
		ID: id,
	}, nil
}

func (c *service) Delete(ctx context.Context, req DeletePubSubRequest) error {
	t, ok := c.pubsubs.Load(req.ID)
	if !ok {
		return nil
	}
	pubsub, ok := t.(*pubsub)
	if !ok {
		return err.Error{
			Code:    500,
			Message: "malformed pubsub type",
			Data: map[string]any{
				"id": req.ID,
			},
		}
	}

	pubsub.mutex.Lock()
	for _, s := range pubsub.subscribers {
		s.cancel()
		close(s.channel)
	}
	c.pubsubs.Delete(req.ID)
	pubsub.mutex.Unlock()
	return nil
}

func (c *service) Publish(ctx context.Context, req PublishRequest) (*PublishResponse, error) {
	_, err := c.publish(req.PubSubID, req.Event)
	if err != nil {
		return nil, err
	}

	return &PublishResponse{
		ID: c.idgen.NextID(),
	}, nil
}

func (c *service) Subscribe(ctx context.Context, req SubscribeRequest) (*SubscribeResponse, error) {
	t, ok := c.pubsubs.Load(req.PubSubID)
	if !ok {
		return nil, err.Error{
			Code:    404,
			Message: "pubsub not found",
			Data: map[string]any{
				"id": req.PubSubID,
			},
		}
	}

	pubsub, ok := t.(*pubsub)
	if !ok || pubsub == nil {
		return nil, err.Error{
			Code:    500,
			Message: "malformed pubsub",
			Data: map[string]any{
				"id": req.PubSubID,
			},
		}
	}

	id := c.idgen.NextID()

	freshCtx, freshCancel := context.WithCancel(context.Background())
	subscriber := subscriber{
		channel: make(chan any),
		id:      id,
		ctx:     freshCtx,
		cancel:  freshCancel,
	}

	pubsub.mutex.Lock()
	pubsub.subscribers = append(pubsub.subscribers, subscriber)
	pubsub.mutex.Unlock()

	return &SubscribeResponse{
		ID:     subscriber.id,
		Events: subscriber.channel,
	}, nil
}

func (c *service) Unsubscribe(ctx context.Context, req UnsubscribeRequest) error {
	t, ok := c.pubsubs.Load(req.PubSubID)
	if !ok {
		return err.Error{
			Code:    404,
			Message: "pubsub not found",
			Data: map[string]any{
				"id": req.PubSubID,
			},
		}
	}

	pubsub, ok := t.(*pubsub)
	if !ok || pubsub == nil {
		return err.Error{
			Code:    500,
			Message: "malformed pubsub",
			Data: map[string]any{
				"id": req.PubSubID,
			},
		}
	}

	pubsub.mutex.Lock()
	for i := 0; i < len(pubsub.subscribers); i++ {
		if pubsub.subscribers[i].id == req.ID {
			ch, cancel := pubsub.subscribers[i].channel, pubsub.subscribers[i].cancel
			defer close(ch)
			defer cancel()

			pubsub.subscribers[i], pubsub.subscribers[len(pubsub.subscribers)-1] = pubsub.subscribers[len(pubsub.subscribers)-1], pubsub.subscribers[i]
			pubsub.subscribers = pubsub.subscribers[0 : len(pubsub.subscribers)-1]
			break
		}
	}
	pubsub.mutex.Unlock()
	return nil
}

func (c *service) publish(id int64, e any) (int, error) {
	t, ok := c.pubsubs.Load(id)
	if !ok {
		return 0, err.Error{
			Code:    404,
			Message: "pubsub not found",
			Data: map[string]any{
				"id": id,
			},
		}
	}

	pubsub, ok := t.(*pubsub)
	if !ok {
		return 0, err.Error{
			Code:    500,
			Message: "malformed pubsub, please create another pubsub",
			Data: map[string]any{
				"id": id,
			},
		}
	}

	pubsub.mutex.RLock()
	subscribers := make([]subscriber, len(pubsub.subscribers))
	copy(subscribers, pubsub.subscribers)
	pubsub.mutex.RUnlock()

	go func(e any, subscribers []subscriber) {
		timeoutDuration := c.cfg.MaxDurationForSubscriberToReceive
		opCtx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
		defer cancel()
		wg := sync.WaitGroup{}
		for _, s := range subscribers {
			wg.Add(1)
			go func(s subscriber) {
				defer wg.Done()

				defer func() {
					if r := recover(); r != nil {
						zlog.Warn().
							Int64("subscriber_id", s.id).
							Msg(_logPrefix + "recovered from panic (likely send on closed channel)")
					}
				}()

				err := publishWithTimeout(opCtx, s.ctx, s.channel, e)
				if err != nil {
					zlog.Error().Err(err).Dur("timeout", timeoutDuration).
						Msg(_logPrefix + "failed to send message to subscriber within the given timeout duration")
				}
			}(s)
		}
		wg.Wait()
	}(e, subscribers)

	return len(subscribers), nil
}

// utility functions

func publishWithTimeout(
	opCtx context.Context, // For operation timeout
	subCtx context.Context, // For subscriber lifetime
	ch chan any,
	e any,
) error {

	select {
	case ch <- e:
		return nil // Success
	case <-opCtx.Done():
		// Timed out from the publisher's side
		return opCtx.Err() // e.g., "context deadline exceeded"
	case <-subCtx.Done():
		// The subscriber went away and canceled its context
		return fmt.Errorf("subscriber canceled: %w", subCtx.Err())
	}
}
