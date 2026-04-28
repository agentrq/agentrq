package err

import "net/http"

type (
	ErrorCode int

	Error struct {
		Code    ErrorCode
		Message string
		Data    map[string]any
	}
)

func (e Error) Error() string {
	return e.Message
}

const (
	ErrorCodeBadRequest                    = http.StatusBadRequest
	ErrorCodeUnauthorized                  = http.StatusUnauthorized
	ErrorCodePaymentRequired               = http.StatusPaymentRequired
	ErrorCodeForbidden                     = http.StatusForbidden
	ErrorCodeNotFound                      = http.StatusNotFound
	ErrorCodeMethodNotAllowed              = http.StatusMethodNotAllowed
	ErrorCodeNotAcceptable                 = http.StatusNotAcceptable
	ErrorCodeProxyAuthRequired             = http.StatusProxyAuthRequired
	ErrorCodeRequestTimeout                = http.StatusRequestTimeout
	ErrorCodeConflict                      = http.StatusConflict
	ErrorCodeGone                          = http.StatusGone
	ErrorCodeLengthRequired                = http.StatusLengthRequired
	ErrorCodePreconditionFailed            = http.StatusPreconditionFailed
	ErrorCodeRequestEntityTooLarge         = http.StatusRequestEntityTooLarge
	ErrorCodeRequestURITooLong             = http.StatusRequestURITooLong
	ErrorCodeUnsupportedMediaType          = http.StatusUnsupportedMediaType
	ErrorCodeRequestedRangeNotSatisfiable  = http.StatusRequestedRangeNotSatisfiable
	ErrorCodeExpectationFailed             = http.StatusExpectationFailed
	ErrorCodeMisdirectedRequest            = http.StatusMisdirectedRequest
	ErrorCodeUnprocessableEntity           = http.StatusUnprocessableEntity
	ErrorCodeLocked                        = http.StatusLocked
	ErrorCodeFailedDependency              = http.StatusFailedDependency
	ErrorCodeTooEarly                      = http.StatusTooEarly
	ErrorCodeUpgradeRequired               = http.StatusUpgradeRequired
	ErrorCodePreconditionRequired          = http.StatusPreconditionRequired
	ErrorCodeTooManyRequests               = http.StatusTooManyRequests
	ErrorCodeRequestHeaderFieldsTooLarge   = http.StatusRequestHeaderFieldsTooLarge
	ErrorCodeUnavailableForLegalReasons    = http.StatusUnavailableForLegalReasons
	ErrorCodeInternalServerError           = http.StatusInternalServerError
	ErrorCodeNotImplemented                = http.StatusNotImplemented
	ErrorCodeBadGateway                    = http.StatusBadGateway
	ErrorCodeServiceUnavailable            = http.StatusServiceUnavailable
	ErrorCodeGatewayTimeout                = http.StatusGatewayTimeout
	ErrorCodeHTTPVersionNotSupported       = http.StatusHTTPVersionNotSupported
	ErrorCodeVariantAlsoNegotiates         = http.StatusVariantAlsoNegotiates
	ErrorCodeInsufficientStorage           = http.StatusInsufficientStorage
	ErrorCodeLoopDetected                  = http.StatusLoopDetected
	ErrorCodeNotExtended                   = http.StatusNotExtended
	ErrorCodeNetworkAuthenticationRequired = http.StatusNetworkAuthenticationRequired
)
