package backfill

import (
	"context"
	"encoding/json"

	zlog "github.com/rs/zerolog/log"
)

// legacyMetadataKeyRenames maps deprecated snake_case message-metadata keys to
// their camelCase replacements. All AgentRQ-owned JSON keys must be camelCase;
// these keys predate that convention and are migrated by
// backfillMessageMetadataKeys.
//
// NOTE: this intentionally does NOT include the Claude Channels protocol keys
// (e.g. the permission verdict / channel notification meta), which are dictated
// by Claude and must remain snake_case.
var legacyMetadataKeyRenames = map[string]string{
	"request_id":       "requestId",
	"tool_name":        "toolName",
	"input_preview":    "inputPreview",
	"slack_user":       "slackUser",
	"decided_in_slack": "decidedInSlack",
	"slack_user_id":    "slackUserId",
	"slack_user_name":  "slackUserName",
}

// backfillMessageMetadataKeys rewrites legacy snake_case keys in message
// metadata to camelCase. It is idempotent: it only loads rows whose metadata
// still contains a legacy key, and skips rows already converted.
func (c *controller) backfillMessageMetadataKeys(ctx context.Context) error {
	keys := make([]string, 0, len(legacyMetadataKeyRenames))
	for k := range legacyMetadataKeyRenames {
		keys = append(keys, k)
	}

	msgs, err := c.repo.SystemListMessagesWithMetadataKeys(ctx, keys)
	if err != nil {
		return err
	}

	updated := 0
	for _, m := range msgs {
		if len(m.Metadata) == 0 {
			continue
		}
		var meta map[string]any
		if err := json.Unmarshal(m.Metadata, &meta); err != nil {
			continue // non-object metadata; leave it untouched
		}

		changed := false
		for snake, camel := range legacyMetadataKeyRenames {
			v, ok := meta[snake]
			if !ok {
				continue
			}
			// Don't clobber a camelCase value that already exists.
			if _, exists := meta[camel]; !exists {
				meta[camel] = v
			}
			delete(meta, snake)
			changed = true
		}
		if !changed {
			continue
		}

		b, err := json.Marshal(meta)
		if err != nil {
			continue
		}
		if err := c.repo.SystemUpdateMessageMetadata(ctx, m.ID, b); err != nil {
			return err
		}
		updated++
	}

	if updated > 0 {
		zlog.Info().Int("messages", updated).Msg("backfilled legacy snake_case message metadata keys to camelCase")
	}
	return nil
}
