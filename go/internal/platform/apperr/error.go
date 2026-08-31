package apperr

import (
	"errors"
	"fmt"
)

const CodeNotPending = "not_pending"

// Error 是面向用户的失败，HTTP 状态码固定，文案进 {"detail": "..."}。
// Code 非空时同时写入 {"code": "..."}，给协议竞争使用稳定机器码。
type Error struct {
	Status int
	Detail string
	Code   string
}

func (e Error) Error() string { return e.Detail }

func Validation(detail string) Error { return Error{Status: 400, Detail: detail} }

func Forbidden(detail string) Error { return Error{Status: 403, Detail: detail} }

func NotFound(detail string) Error { return Error{Status: 404, Detail: detail} }

func Conflict(detail string) Error { return Error{Status: 409, Detail: detail} }

func ConflictCode(code, detail string) Error {
	return Error{Status: 409, Detail: detail, Code: code}
}

func NotPending(detail string) Error {
	return ConflictCode(CodeNotPending, detail)
}

func Gone(detail string) Error { return Error{Status: 410, Detail: detail} }

func TooLarge(detail string) Error { return Error{Status: 413, Detail: detail} }

func UnsupportedMedia(detail string) Error { return Error{Status: 415, Detail: detail} }

func Busy(detail string) Error { return Error{Status: 429, Detail: detail} }

func Unavailable(detail string) Error { return Error{Status: 503, Detail: detail} }

func Internal(detail string) Error { return Error{Status: 500, Detail: detail} }

func IsNotFound(err error) bool {
	var e Error
	return errors.As(err, &e) && e.Status == 404
}

func Validationf(format string, args ...any) Error {
	return Validation(fmt.Sprintf(format, args...))
}
