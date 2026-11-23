// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package pubsub

type (
	CreatePubSubRequest struct {
		ID int64
	}

	CreatePubSubResponse struct {
		ID int64
	}

	DeletePubSubRequest struct {
		ID int64
	}

	PublishRequest struct {
		PubSubID int64
		Event    any
	}

	PublishResponse struct {
		ID int64
	}

	SubscribeRequest struct {
		PubSubID int64
	}

	SubscribeResponse struct {
		ID     int64
		Events chan any
	}

	UnsubscribeRequest struct {
		PubSubID int64
		ID       int64
	}
)
