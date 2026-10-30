// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package err

import (
	"errors"
	"testing"
)

func TestErrorIsItsMessage(t *testing.T) {
	var e error = Error{Code: ErrorCodeNotFound, Message: "workspace not found", Data: map[string]any{"id": "x"}}
	if e.Error() != "workspace not found" {
		t.Errorf("got %q", e.Error())
	}
	var target Error
	if !errors.As(e, &target) || target.Code != ErrorCodeNotFound {
		t.Errorf("as Error = %+v", target)
	}
}
