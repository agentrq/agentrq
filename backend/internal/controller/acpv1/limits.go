// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import "time"

// Every bound on one native connection. The durations and the per-session
// bounds are variables so that tests can shrink them; a connection copies them
// when it is created, so a shrink never races an older one.
const (
	maxFrameBytes     = 16 << 20 // one ACP message; larger closes with 1009
	maxConnsPerWS     = 4        // live connections per workspace on this instance
	maxConnsPerUser   = 16       // live connections per user on this instance
	updateQueueCap    = 4096     // notifications waiting to be applied, in order
	outboundQueueCap  = 1024     // frames waiting for the socket writer
	replayMaxFrames   = 1024     // unacknowledged frames kept for a resume
	replayMaxBytes    = 8 << 20  // the same, by size
	maxPendingCalls   = 256      // our requests waiting for an answer
	maxServedRequests = 64       // the agent's requests we are still answering
	maxConcurrency    = 64       // turns one connection runs at once
	ackEveryFrames    = 64       // acknowledge after this many frames at most
)

var (
	ackEvery              = time.Second      // acknowledge at least this often
	pingEvery             = 25 * time.Second // keeps proxies from closing an idle socket
	pongWait              = 60 * time.Second // a socket silent this long is dead
	writeWait             = 10 * time.Second // one frame's write deadline
	graceWindow           = 120 * time.Second
	initializeTimeout     = 60 * time.Second // then the bridge is asked to restart the agent
	idleSessionWait       = 5 * time.Minute
	callTimeout           = 60 * time.Second // our requests other than session/prompt
	cancelSettleWait      = 5 * time.Second  // a cancelled turn's answer
	defaultPermissionWait = 30 * time.Minute // unless the bridge's X-AgentRQ-Permission-Timeout says otherwise

	maxSessions      = 32        // ACP sessions per connection before LRU eviction
	maxReplyBytes    = 1 << 20   // one turn's answer, cut with a marker past it
	maxThoughtBytes  = 256 << 10 // one block of reasoning
	maxToolCallsKept = 512       // per session, unfinished calls only
	maxEarlyUpdates  = 256       // updates held for a session not yet registered
)
