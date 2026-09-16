// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import "encoding/json"

// jsonUnmarshal is named so the manifest reader's intent is legible and so
// the only JSON decision in this package — what to do with unknown fields —
// lives in one place.
//
// Unknown fields are ignored. A newer publisher adding one must not stop an
// older daemon from reading the version and the artefact it already
// understands, and the signature covers what is actually used rather than
// whatever arrived.
func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
