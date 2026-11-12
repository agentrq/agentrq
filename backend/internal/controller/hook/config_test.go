// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hook

import (
	"strings"
	"testing"
	"time"
)

func TestSettingsDefaultsForZeroValues(t *testing.T) {
	s, err := Config{}.Settings()
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{DeliveryRetention: 7 * 24 * time.Hour, MaxPayloadSize: 65536}
	if s != want {
		t.Errorf("settings = %+v, want %+v", s, want)
	}
}

func TestSettingsReadsEveryField(t *testing.T) {
	s, err := Config{AllowUserHooks: true, DeliveryRetention: "36h", MaxPayloadSize: 1024}.Settings()
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{AllowUserHooks: true, DeliveryRetention: 36 * time.Hour, MaxPayloadSize: 1024}
	if s != want {
		t.Errorf("settings = %+v, want %+v", s, want)
	}
}

func TestSettingsAcceptsTheBounds(t *testing.T) {
	for _, c := range []Config{
		{DeliveryRetention: "1d", MaxPayloadSize: MinPayloadSize},
		{DeliveryRetention: "24h", MaxPayloadSize: MaxRequestBodySize},
	} {
		if _, err := c.Settings(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestSettingsRefusesBadValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"retention not a duration", Config{DeliveryRetention: "week"}, "hooks.deliveryRetention"},
		{"retention under a day", Config{DeliveryRetention: "23h"}, "at least 1d"},
		{"payload under the minimum", Config{MaxPayloadSize: MinPayloadSize - 1}, "hooks.maxPayloadSize"},
		{"payload over the request cap", Config{MaxPayloadSize: MaxRequestBodySize + 1}, "hooks.maxPayloadSize"},
		{"negative payload", Config{MaxPayloadSize: -1}, "hooks.maxPayloadSize"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.cfg.Settings()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one naming %q", err, tc.want)
			}
		})
	}
}
