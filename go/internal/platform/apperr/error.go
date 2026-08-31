// Package apperr 定义面向用户的失败。httpx.AbortErr 把 Status 写成 HTTP 状态，Detail 进 {"detail"}；
// Code 非空时再写 {"code"}，给前端/协议竞争用稳定机器码。不要把内部 error 字符串直接丢给用户。
package apperr

import (
	"errors"
	"fmt"
)

// CodeNotPending 是目标不在 pending 时的稳定机器码，随 409 写入 {"code"}。
const CodeNotPending = "not_pending"

// CodeEventSequenceConflict 是 Turn 事件序号竞争的稳定机器码，随 409 写入 {"code"}。
const CodeEventSequenceConflict = "event_sequence_conflict"

// Error 是面向用户的失败，HTTP 状态码固定，文案进 {"detail": "..."}。
// Code 非空时同时写入 {"code": "..."}，给协议竞争使用稳定机器码。
type Error struct {
	Status int
	Detail string
	Code   string // 非空时写入 {"code"}，给协议竞争用稳定机器码
}

// Error 实现 [error]，把 Detail 交给日志/errors.Is 链；HTTP 面仍读 Status/Code 字段。
func (e Error) Error() string { return e.Detail }

// Validation 表示请求体或业务校验失败（400）。detail 必须是可给用户看的中文。
func Validation(detail string) Error { return Error{Status: 400, Detail: detail} }

// Forbidden 表示已认证但无权做这件事（403），不是「请先登录」。
func Forbidden(detail string) Error { return Error{Status: 403, Detail: detail} }

// NotFound 表示资源不存在（404）。列表里「没有下一页」不要用这个。
func NotFound(detail string) Error { return Error{Status: 404, Detail: detail} }

// Conflict 表示状态不允许（409），例如 revision 已变。需要机器码时用 [ConflictCode]。
func Conflict(detail string) Error { return Error{Status: 409, Detail: detail} }

// ConflictCode 是带稳定 Code 的 409，给协议重试/竞争（如 not_pending）区分原因。
func ConflictCode(code, detail string) Error {
	return Error{Status: 409, Detail: detail, Code: code}
}

// NotPending 是目标已不在 pending 的 409，Code 固定 [CodeNotPending]。
func NotPending(detail string) Error {
	return ConflictCode(CodeNotPending, detail)
}

// Gone 表示资源曾经存在但已不可用（410），例如会话已销毁。
// 调用时机：明确「以前有、现在没了」。单纯找不到用 [NotFound]（404），不要用 410 表达列表空页。
func Gone(detail string) Error { return Error{Status: 410, Detail: detail} }

// TooLarge 表示上传或请求体超过上限（413）。
// 调用时机：单文件或批量字节超限。格式不对用 [UnsupportedMedia]（415），不要用 413 表达 MIME 不允许。
func TooLarge(detail string) Error { return Error{Status: 413, Detail: detail} }

// UnsupportedMedia 表示 MIME/格式不在允许集（415）。
func UnsupportedMedia(detail string) Error { return Error{Status: 415, Detail: detail} }

// Busy 表示限流或别人正占用同一聚合（429）。
// 调用时机：进程锁/advisory lock 没拿到。不要把供应商限流文案直接映射成 429——那是业务 failed/unknown。
func Busy(detail string) Error { return Error{Status: 429, Detail: detail} }

// Unavailable 表示依赖暂时不可用（503），可以稍后重试。
// 调用时机：供应商尚未配置、设置解锁令牌未配。不是「请先登录」（那是 401），也不是无权（403）。
func Unavailable(detail string) Error { return Error{Status: 503, Detail: detail} }

// Internal 是不应暴露内部细节的 500。httpx 会把 5xx 记入 Gin error 再写固定文案或 Detail。
func Internal(detail string) Error { return Error{Status: 500, Detail: detail} }

// IsNotFound 用 errors.As 判断是否为 Status=404 的 [Error]，不要用字符串匹配。
func IsNotFound(err error) bool {
	var e Error
	return errors.As(err, &e) && e.Status == 404
}

// Validationf 用 fmt.Sprintf 拼 400 文案。format 同样必须是可给用户看的中文。
func Validationf(format string, args ...any) Error {
	return Validation(fmt.Sprintf(format, args...))
}
