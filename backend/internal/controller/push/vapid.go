// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package push

import (
	"net/http"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// vapidTokenLifetime is how long one signed VAPID header is reused. Apple asks
// for no refresh more often than hourly; the token itself expires in 12 hours.
const vapidTokenLifetime = time.Hour

// vapidTokens sends every push to one push service with the same VAPID header
// for an hour. webpush-go signs a fresh JWT for every push and offers no way to
// pass one in, so the first header it signs for a push service is kept and
// stamped on the requests after it.
type vapidTokens struct {
	next webpush.HTTPClient
	now  func() time.Time

	mu         sync.Mutex
	byAudience map[string]vapidToken
}

type vapidToken struct {
	header string
	until  time.Time
}

func newVAPIDTokens(next webpush.HTTPClient) *vapidTokens {
	return &vapidTokens{next: next, now: time.Now, byAudience: map[string]vapidToken{}}
}

// Do sends req with the push service's current VAPID header. The JWT's
// audience is the endpoint's origin, so that is what a header is kept by.
func (v *vapidTokens) Do(req *http.Request) (*http.Response, error) {
	audience := req.URL.Scheme + "://" + req.URL.Host
	now := v.now()

	v.mu.Lock()
	token, ok := v.byAudience[audience]
	if !ok || !now.Before(token.until) {
		token = vapidToken{header: req.Header.Get("Authorization"), until: now.Add(vapidTokenLifetime)}
		v.byAudience[audience] = token
	}
	v.mu.Unlock()

	req.Header.Set("Authorization", token.header)
	return v.next.Do(req)
}
