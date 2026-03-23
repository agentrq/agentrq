package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hasmcp/agentrq/backend/internal/repository/base"
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
	}

	e := httpError{}
	e.Error.Code = code
	e.Error.Message = msg

	b, _ := json.Marshal(e)
	return b, code
}
