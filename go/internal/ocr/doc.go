// Package ocr 提供成片 PNG 可见文字提取与 text_trace 对照闸（IQ-CF-02 B0）。
//
// 选型（任务证据）：仓库无现成 provider OCR。B0 默认引擎为嵌入 Liberation Sans 的
// 字形模板匹配（对高对比、近似该字体的受控排版成片可测）；可选
// PRODUCTFLOW_OCR_LIVE=1 走 OpenAI 兼容视觉接口。不把 PNG 元数据/旁路文本当 OCR。
package ocr
