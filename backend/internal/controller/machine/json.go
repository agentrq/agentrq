// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import "encoding/json"

// jsonMarshal is a named indirection so the relay's control helper reads
// clearly and so a test can see exactly what was encoded.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
