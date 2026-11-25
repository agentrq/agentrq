// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package base

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The card's data holds a number a float64 cannot: answering must not touch it.
const answerCardMetadata = `{"type":"a2ui","surfaceId":"s","messages":[{"updateDataModel":{"value":12345678901234567}}]}`

func TestAnswerCard(t *testing.T) {
	ctx := context.Background()
	for name, r := range editRepos(t) {
		t.Run(name, func(t *testing.T) {
			if err := r.conn(ctx).Create(&model.Message{ID: 1, TaskID: 10, Metadata: datatypes.JSON(answerCardMetadata)}).Error; err != nil {
				t.Fatal(err)
			}
			read := func(id int64) (model.Message, error) {
				var m model.Message
				return m, r.conn(ctx).First(&m, id).Error
			}
			reply := func(id int64) model.Message {
				return model.Message{ID: id, TaskID: 10, Sender: "human", Text: "Approve"}
			}

			if err := r.AnswerCard(ctx, 99, 1, func(model.Message) (model.Message, []byte, error) {
				t.Error("answer called for another task's message")
				return model.Message{}, nil, nil
			}); !errors.Is(err, ErrNotFound) {
				t.Errorf("another task's message = %v, want ErrNotFound", err)
			}

			refused := errors.New("this card is already answered")
			if err := r.AnswerCard(ctx, 10, 1, func(model.Message) (model.Message, []byte, error) {
				return reply(2), []byte(`{"name":"approve"}`), refused
			}); !errors.Is(err, refused) {
				t.Errorf("refused answer = %v, want its error", err)
			}

			// The reply cannot be stored: the mark is rolled back with it.
			if err := r.AnswerCard(ctx, 10, 1, func(model.Message) (model.Message, []byte, error) {
				return reply(1), []byte(`{"name":"approve"}`), nil
			}); err == nil {
				t.Error("a reply whose id is taken was stored")
			}
			// The mark cannot be written: nothing is stored.
			if err := r.AnswerCard(ctx, 10, 1, func(model.Message) (model.Message, []byte, error) {
				return reply(2), []byte(`{`), nil
			}); err == nil {
				t.Error("a mark that is no JSON was written")
			}
			if m, _ := read(1); !strings.Contains(string(m.Metadata), "12345678901234567") || strings.Contains(string(m.Metadata), "answered") {
				t.Errorf("after failed answers the card is %s, want it unchanged", m.Metadata)
			}
			if _, err := read(2); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Errorf("a failed answer stored its reply: %v", err)
			}

			if err := r.AnswerCard(ctx, 10, 1, func(m model.Message) (model.Message, []byte, error) {
				if !sameJSON(m.Metadata, answerCardMetadata) {
					t.Errorf("answer read %s", m.Metadata)
				}
				return reply(2), []byte(`{"name":"approve","at":"2026-11-25T00:00:00Z"}`), nil
			}); err != nil {
				t.Fatal(err)
			}
			card, _ := read(1)
			var meta struct {
				Type     string `json:"type"`
				Answered struct{ Name, At string }
			}
			if err := json.Unmarshal(card.Metadata, &meta); err != nil || meta.Type != "a2ui" || meta.Answered.Name != "approve" || meta.Answered.At != "2026-11-25T00:00:00Z" {
				t.Errorf("answered card = %s, %v", card.Metadata, err)
			}
			if !strings.Contains(string(card.Metadata), "12345678901234567") {
				t.Errorf("answering changed the card's data: %s", card.Metadata)
			}
			if m, err := read(2); err != nil || m.Text != "Approve" || m.Sender != "human" {
				t.Errorf("reply = %+v, %v", m, err)
			}

			if err := r.conn(ctx).Migrator().DropTable(&model.Message{}); err != nil {
				t.Fatal(err)
			}
			if err := r.AnswerCard(ctx, 10, 1, func(model.Message) (model.Message, []byte, error) {
				t.Error("answer called without a message")
				return model.Message{}, nil, nil
			}); err == nil || errors.Is(err, ErrNotFound) {
				t.Errorf("with the database unavailable = %v, want its error", err)
			}
		})
	}
}

// Of many clicks at once, each reads the card as the one before it left it:
// one answers, every other is refused, and one reply is stored.
func TestAnswerCard_ConcurrentClicksAnswerOnce(t *testing.T) {
	ctx := context.Background()
	for name, r := range editRepos(t) {
		t.Run(name, func(t *testing.T) {
			if err := r.conn(ctx).Create(&model.Message{ID: 1, TaskID: 10, Metadata: datatypes.JSON(answerCardMetadata)}).Error; err != nil {
				t.Fatal(err)
			}
			answered := errors.New("answered")
			const clicks = 20
			var wg sync.WaitGroup
			errs := make(chan error, clicks)
			for i := range clicks {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs <- r.AnswerCard(ctx, 10, 1, func(m model.Message) (model.Message, []byte, error) {
						if strings.Contains(string(m.Metadata), "answered") {
							return model.Message{}, nil, answered
						}
						return model.Message{ID: int64(100 + i), TaskID: 10, Sender: "human"}, []byte(`{"name":"approve"}`), nil
					})
				}()
			}
			wg.Wait()
			close(errs)
			won := 0
			for err := range errs {
				switch {
				case err == nil:
					won++
				case !errors.Is(err, answered):
					t.Fatal(err)
				}
			}
			var replies int64
			if err := r.conn(ctx).Model(&model.Message{}).Where("task_id = ? AND sender = ?", 10, "human").Count(&replies).Error; err != nil {
				t.Fatal(err)
			}
			if won != 1 || replies != 1 {
				t.Errorf("%d clicks answered and %d replies stored, want 1 and 1", won, replies)
			}
		})
	}
}

// Without a Postgres to run on, the Postgres form is at least the one a
// Postgres connection picks.
func TestAnswerCard_PostgresPicksItsOwnSQL(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 port=1"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := setAnsweredExpr(db); got != pgSetAnswered {
		t.Errorf("expression = %q, want the Postgres one", got)
	}
}
