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
