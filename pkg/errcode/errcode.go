package errcode

import "fmt"

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func New(code int, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

var (
	OK                = New(0, "ok")
	InvalidParam      = New(40001, "invalid parameter")
	NotFound          = New(40401, "resource not found")
	Conflict          = New(40901, "resource conflict")
	Internal          = New(50001, "internal error")
	ChannelNotFound   = New(40402, "channel not registered")
	AudienceEmpty     = New(40002, "audience is empty")
	UnsupportedScene  = New(40003, "unsupported biz scene")
	NothingToRetry    = New(40004, "nothing to retry")
	InvalidState      = New(40902, "invalid task state for this operation")
	TemplateNotUsable = New(40903, "template not approved or disabled")
	QuotaExceeded     = New(42901, "channel quota exceeded")
)
