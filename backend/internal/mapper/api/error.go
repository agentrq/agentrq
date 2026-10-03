// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
)

type httpError struct {
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func FromErrorToHTTPResponse(err error) ([]byte, int) {
	code := http.StatusInternalServerError
	msg := "internal server error"

	var forkErr *entity.ForkError
	if errors.Is(err, base.ErrNotFound) {
		code = http.StatusNotFound
		msg = "not found"
	} else if errors.As(err, &forkErr) {
		code = forkErrorStatus(forkErr.Kind)
		msg = forkErr.Message
	} else if errors.Is(err, entity.ErrTaskTitleLocked) {
		code = http.StatusConflict
		msg = err.Error()
	} else if err.Error() == "rate limit exceeded" {
		code = http.StatusTooManyRequests
		msg = "rate limit exceeded"
	}

	e := httpError{}
	e.Error.Code = code
	e.Error.Message = msg

	b, _ := json.Marshal(e)
	return b, code
}

// FromMessageToHTTPResponse renders an error whose text is meant for the user.
//
// FromErrorToHTTPResponse deliberately replaces unrecognized errors with a
// generic message so internals never leak; this is the opt-in for the cases
// where the message *is* the point — a rejected workflow cycle has to say which
// connection was refused, or the editor can only report that something failed.
func FromMessageToHTTPResponse(message string, code int) []byte {
	e := httpError{}
	e.Error.Code = code
	e.Error.Message = message
	b, _ := json.Marshal(e)
	return b
}

// forkErrorStatus answers a refused fork or merge: 409 when the workspace is
// fine and its state is what stands in the way, 422 when the request can never
// succeed as asked.
func forkErrorStatus(kind error) int {
	switch kind {
	case entity.ErrHasForks, entity.ErrForkUnfinished, entity.ErrForkNoDelete, entity.ErrForkAgentRunning:
		return http.StatusConflict
	}
	return http.StatusUnprocessableEntity
}
