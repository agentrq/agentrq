package api

import (
	"encoding/json"
	"errors"
	"net/http"

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

	if errors.Is(err, base.ErrNotFound) {
		code = http.StatusNotFound
		msg = "not found"
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
