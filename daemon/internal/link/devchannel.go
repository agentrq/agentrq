// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package link

import (
	"strings"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/stream"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
)

// How long a Claude Code session's screen is watched for the
// development-channels warning, how often it is read, how long a press is
// given to take before Enter is pressed again, and how many presses are tried.
var (
	devChannelWait    = 2 * time.Minute
	devChannelPoll    = 250 * time.Millisecond
	devChannelRetry   = time.Second
	devChannelPresses = 3
)

// confirmDevChannel answers Claude Code's development-channels warning when
// the only channel it lists is the one agentrqd passed (#1446). Claude Code
// asks on every start and nothing pre-accepts it, so an unattended session —
// launched, restored or a fork's — would never reach its first task. Any other
// channel list, or no warning within wait, is left for a person.
//
// A press is checked rather than trusted: one that lands before Claude Code
// reads its input is lost, so Enter is pressed again while the warning stays.
func (l *Link) confirmDevChannel(sess *supervisor.Session, screen *stream.Screen, wait time.Duration) {
	if sess.Kind != supervisor.KindClaudeCode {
		return
	}
	want := supervisor.DevChannel(sess.Params)
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(devChannelPoll)
	defer tick.Stop()
	presses := 0
	var pressed time.Time
	for {
		select {
		case <-sess.Ended():
			return
		case <-deadline.C:
			return
		case <-tick.C:
		}
		channels, ok := screen.DevChannelPrompt()
		switch {
		case !ok && presses > 0:
			l.Log.Info("confirmed Claude Code's development-channels warning for the session's own channel",
				"session", sess.ID, "channel", want)
			return
		case !ok:
			continue
		case len(channels) != 1 || channels[0] != want:
			l.Log.Warn("left Claude Code's development-channels warning for a person: it lists channels agentrqd did not pass",
				"session", sess.ID, "channels", strings.Join(channels, " "), "want", want)
			return
		case presses > 0 && time.Since(pressed) < devChannelRetry:
			continue
		case presses == devChannelPresses:
			l.Log.Warn("left Claude Code's development-channels warning for a person: it stayed after Enter was pressed",
				"session", sess.ID, "presses", presses)
			return
		}
		if _, err := sess.PTY().Write([]byte("\r")); err != nil {
			l.Log.Warn("could not confirm Claude Code's development-channels warning", "session", sess.ID, "error", err)
			return
		}
		presses++
		pressed = time.Now()
	}
}
