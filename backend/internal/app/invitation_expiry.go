// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"time"

	zlog "github.com/rs/zerolog/log"
)

// invitationExpirySweep is how often expired team invitations are counted.
const invitationExpirySweep = time.Hour

type expiredInvitationCounter interface {
	CountExpiredInvitations(ctx context.Context) (int, error)
}

// startInvitationExpirySweep counts expired team invitations every interval
// until ctx is done. An expiry happens to no request, so this is its only
// telemetry; every instance sweeps, and the mark keeps each counted once.
func startInvitationExpirySweep(ctx context.Context, c expiredInvitationCounter, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := c.CountExpiredInvitations(ctx); err != nil {
					zlog.Warn().Err(err).Int("counted", n).Msg("team invitation expiry sweep failed")
				}
			}
		}
	}()
}
