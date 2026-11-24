// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package push

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
)

// sentHeaders records the Authorization header of every request it is handed.
type sentHeaders struct{ got []string }

func (s *sentHeaders) Do(req *http.Request) (*http.Response, error) {
	s.got = append(s.got, req.Header.Get("Authorization"))
	return nil, errors.New("not sent")
}

func pushTo(t *testing.T, v *vapidTokens, endpoint, header string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", header)
	_, _ = v.Do(req)
}

func TestVAPIDTokens_ReusesAHeaderForAnHour(t *testing.T) {
	sent := &sentHeaders{}
	v := newVAPIDTokens(sent)
	now := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }

	pushTo(t, v, "https://web.push.apple.com/a", "first")
	now = now.Add(vapidTokenLifetime - time.Second)
	pushTo(t, v, "https://web.push.apple.com/b", "second")
	// Another push service is another audience, with a header of its own.
	pushTo(t, v, "https://fcm.googleapis.com/c", "other")
	now = now.Add(time.Second)
	pushTo(t, v, "https://web.push.apple.com/d", "after an hour")

	want := []string{"first", "first", "other", "after an hour"}
	if len(sent.got) != len(want) {
		t.Fatalf("sent %q, want %q", sent.got, want)
	}
	for i := range want {
		if sent.got[i] != want[i] {
			t.Errorf("push %d sent %q, want %q", i, sent.got[i], want[i])
		}
	}
}

// Through the real library: it signs a new JWT for every push (ECDSA
// signatures differ each time), and the push service must still see one.
func TestSendToUser_SameVAPIDHeaderAcrossPushes(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, mockRepo := newPushTestController(t)
	p256dh, auth := newTestSubscriptionKeys(t)
	mockRepo.EXPECT().
		ListPushSubscriptionsByUserAndWorkspace(gomock.Any(), int64(7), int64(3)).
		Return([]model.PushSubscription{{Endpoint: srv.URL, P256dh: p256dh, Auth: auth}}, nil).
		Times(2)

	c.sendToUser(context.Background(), 7, 3, 0, PushTypeTaskCreate, pushPayload{Title: "one"})
	c.sendToUser(context.Background(), 7, 3, 0, PushTypeTaskCreate, pushPayload{Title: "two"})

	if len(got) != 2 || got[0] == "" || got[0] != got[1] {
		t.Errorf("Authorization headers %q, want the same one twice", got)
	}
}
