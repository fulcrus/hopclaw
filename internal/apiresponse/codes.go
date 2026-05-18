package apiresponse

type ErrorCode string

const (
	ErrorCodeRequestFailed       ErrorCode = "request_failed"
	ErrorCodeInvalidArgument     ErrorCode = "invalid_argument"
	ErrorCodeInvalidJSON         ErrorCode = "invalid_json"
	ErrorCodeRequestBodyTooLarge ErrorCode = "request_body_too_large"
	ErrorCodeUnauthenticated     ErrorCode = "unauthenticated"
	ErrorCodeAuthorizationDenied ErrorCode = "authorization_denied"
	ErrorCodeNotFound            ErrorCode = "not_found"
	ErrorCodeConflict            ErrorCode = "conflict"
	ErrorCodeRateLimited         ErrorCode = "rate_limited"
	ErrorCodeServiceUnavailable  ErrorCode = "service_unavailable"
	ErrorCodeInternalError       ErrorCode = "internal_error"
)

func AllErrorCodes() []string {
	return []string{
		string(ErrorCodeRequestFailed),
		string(ErrorCodeInvalidArgument),
		string(ErrorCodeInvalidJSON),
		string(ErrorCodeRequestBodyTooLarge),
		string(ErrorCodeUnauthenticated),
		string(ErrorCodeAuthorizationDenied),
		string(ErrorCodeNotFound),
		string(ErrorCodeConflict),
		string(ErrorCodeRateLimited),
		string(ErrorCodeServiceUnavailable),
		string(ErrorCodeInternalError),
	}
}

func DefaultHTTPErrorCode(status int) ErrorCode {
	switch status {
	case 400, 405, 422:
		return ErrorCodeInvalidArgument
	case 401:
		return ErrorCodeUnauthenticated
	case 403:
		return ErrorCodeAuthorizationDenied
	case 404:
		return ErrorCodeNotFound
	case 409:
		return ErrorCodeConflict
	case 413:
		return ErrorCodeRequestBodyTooLarge
	case 429:
		return ErrorCodeRateLimited
	case 503:
		return ErrorCodeServiceUnavailable
	default:
		if status >= 500 {
			return ErrorCodeInternalError
		}
		return ErrorCodeRequestFailed
	}
}
