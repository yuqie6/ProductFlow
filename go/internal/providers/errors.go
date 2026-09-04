package providers

import "errors"

var (
	// ErrUnknown 表示供应商结果无法证明。调用方标 unknown，不得当失败自动重试。
	ErrUnknown = errors.New("供应商请求结果未知")
	// ErrTimeout 表示请求超时，结果不可证明。
	ErrTimeout = errors.New("图片供应商请求超时，请稍后重试")
	// ErrConnection 表示连接中断，结果不可证明。
	ErrConnection = errors.New("图片供应商连接中断，请检查网络或代理后重试")
	// ErrProvider5xx 表示供应商 5xx，结果不可证明。
	ErrProvider5xx = errors.New("图片供应商服务异常，请稍后重试")
	// ErrRateLimit 表示供应商限流或配额不足，视为已证明失败且可重试。
	ErrRateLimit = errors.New("图片供应商限流或配额不足，请稍后重试或降低并发后再试")
	// ErrTextOutput 表示供应商返回文字而非图片，视为已证明失败。
	ErrTextOutput = errors.New("图片供应商已完成请求，但返回的是文字回复，没有返回图片结果")
	// ErrMissingOutput 表示供应商完成但没有返回图片。
	ErrMissingOutput = errors.New("图片供应商没有返回图片结果，请稍后重试")
)
