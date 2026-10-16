// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

func TestElicitation(t *testing.T) {
	form := `{"sessionId":"sess-1","mode":"form","message":"Which branch?","requestedSchema":{"type":"object","properties":{"branch":{"type":"string"}}}}`
	answers := func(a agentlink.Answer, err error) func(context.Context, agentlink.Question) (agentlink.Answer, error) {
		return func(context.Context, agentlink.Question) (agentlink.Answer, error) { return a, err }
	}

	t.Run("a form question is put to the human and the answer returned", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		rec.read(func() {
			rec.ask = answers(agentlink.Answer{Action: "accept", Content: map[string]any{"branch": "main"}}, nil)
		})
		a.request(5, methodElicitationCreate, form)
		if got := a.answerTo(5); string(got.Result) != `{"action":"accept","content":{"branch":"main"}}` {
			t.Fatalf("the agent was answered %s, want the accepted form", got.Result)
		}
		rec.read(func() {
			q := rec.questions[0].q
			if q.Mode != "form" || q.Message != "Which branch?" || q.RequestedSchema["type"] != "object" {
				t.Fatalf("the human was asked %+v, want the agent's form", q)
			}
		})
	})

	t.Run("a question with a session goes to that session's task", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodElicitationCreate, form)
		a.answerTo(5)
		rec.read(func() {
			if len(rec.questions) != 1 || rec.questions[0].taskID != 42 || len(rec.humanTasks) != 0 {
				t.Fatalf("questions were %+v and human tasks %v, want one question in task 42 and no new task", rec.questions, rec.humanTasks)
			}
		})
	})

	t.Run("a url question during login opens a human task named after the message", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodElicitationCreate, string(fixture(t, "elicitation_create.url.request.json")))
		if got := a.answerTo(5); string(got.Result) != `{"action":"cancel"}` {
			t.Fatalf("the agent was answered %s, want the recorder's cancel", got.Result)
		}
		rec.read(func() {
			if !slices.Equal(rec.humanTasks, []string{"Enter code ABCD-1234|Enter code ABCD-1234"}) {
				t.Fatalf("human tasks were %v, want one named after the message", rec.humanTasks)
			}
			if q := rec.questions[0]; q.taskID != 77 || q.q.Mode != "url" || q.q.URL != "https://auth.example/device" {
				t.Fatalf("the human was asked %+v, want the url in the new task 77", q)
			}
		})
	})

	t.Run("a question whose task cannot be created is answered cancel", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		rec.read(func() { rec.failCreate = errors.New("database unavailable") })
		a.request(5, methodElicitationCreate, string(fixture(t, "elicitation_create.url.request.json")))
		if got := a.answerTo(5); string(got.Result) != `{"action":"cancel"}` {
			t.Fatalf("the agent was answered %s, want cancel", got.Result)
		}
		rec.read(func() {
			if len(rec.questions) != 0 {
				t.Fatalf("the human was asked %+v, want nothing with no task to ask in", rec.questions)
			}
		})
	})

	t.Run("elicitation/complete closes a url question still waiting", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		asked := make(chan struct{})
		rec.read(func() {
			rec.ask = func(ctx context.Context, _ agentlink.Question) (agentlink.Answer, error) {
				close(asked)
				<-ctx.Done()
				return agentlink.Answer{}, ctx.Err()
			}
		})
		a.request(5, methodElicitationCreate, string(fixture(t, "elicitation_create.url.request.json")))
		<-asked
		a.write(`{"jsonrpc":"2.0","method":"elicitation/complete","params":{"elicitationId":"nope"}}`)
		a.write(`{"jsonrpc":"2.0","method":"elicitation/complete","params":"not an object"}`)
		a.write(`{"jsonrpc":"2.0","method":"elicitation/complete","params":{"elicitationId":"e-1"}}`)
		if got := a.answerTo(5); string(got.Result) != `{"action":"cancel"}` {
			t.Fatalf("the agent was answered %s, want cancel", got.Result)
		}
	})

	t.Run("an invalid schema is answered cancel", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		rec.read(func() { rec.ask = answers(agentlink.Answer{}, errors.New("invalid requestedSchema")) })
		a.request(5, methodElicitationCreate, form)
		if got := a.answerTo(5); string(got.Result) != `{"action":"cancel"}` {
			t.Fatalf("the agent was answered %s, want cancel", got.Result)
		}
	})

	t.Run("a mode AgentRQ did not advertise is answered -32602", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodElicitationCreate, `{"sessionId":"sess-1","mode":"voice","message":"Say it"}`)
		if got := a.answerTo(5); got.Error == nil || got.Error.Code != codeInvalidParams {
			t.Fatalf("the agent was answered %+v, want error -32602", got)
		}
		rec.read(func() {
			if len(rec.questions) != 0 {
				t.Fatalf("the human was asked %+v, want nothing", rec.questions)
			}
		})
	})

	t.Run("a declined form carries no content", func(t *testing.T) {
		rec, a, _ := midTurn(t, agentOpts{})
		rec.read(func() {
			rec.ask = answers(agentlink.Answer{Action: "decline", Content: map[string]any{"branch": "x"}}, nil)
		})
		a.request(5, methodElicitationCreate, form)
		if got := a.answerTo(5); string(got.Result) != `{"action":"decline"}` {
			t.Fatalf("the agent was answered %s, want a bare decline", got.Result)
		}
	})

	t.Run("a question that cannot be read is answered -32602", func(t *testing.T) {
		_, a, _ := midTurn(t, agentOpts{})
		a.request(5, methodElicitationCreate, `"not an object"`)
		if got := a.answerTo(5); got.Error == nil || got.Error.Code != codeInvalidParams {
			t.Fatalf("the agent was answered %+v, want error -32602", got)
		}
	})
}
