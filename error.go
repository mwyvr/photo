package photo

import "fmt"

const (
	ECONFLICT       = "conflict"        // duplicate resource
	EINTERNAL       = "internal"        // unexpected internal error
	EINVALID        = "invalid"         // validation failure
	ENOTFOUND       = "not_found"       // resource does not exist
	EUNAUTHORIZED   = "unauthorized"    // not authenticated
	EFORBIDDEN      = "forbidden"       // authenticated but not permitted
	ENOTIMPLEMENTED = "not_implemented"
)

// Error is an application-level error with a machine-readable code and a
// human-readable message. HTTP handlers map Code to a status code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("photo error: code=%s message=%s", e.Code, e.Message)
}

// Errorf constructs an *Error.
func Errorf(code, format string, args ...interface{}) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrorCode unwraps the application error code, or returns EINTERNAL.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return EINTERNAL
}

// ErrorMessage unwraps the human-readable message, or returns "Internal error."
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if e, ok := err.(*Error); ok {
		return e.Message
	}
	return "Internal error."
}
