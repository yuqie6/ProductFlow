package media

import (
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// 生成路径字节预算。与 [DefaultLimits] 的 10MiB / 50MiB 一致，但不读上传设置覆盖。
const (
	GenerationMaxImageBytes = 10 * 1024 * 1024 // 单张输出
	GenerationMaxBatchBytes = 50 * 1024 * 1024 // 聚合输入或整批输出
)

// SumBytes 累加各切片长度；nil 当 0。给生成闸门用，不做拷贝。
func SumBytes(parts [][]byte) int {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	return total
}

// RejectGenerationInput 在打供应商前拒绝合计超过 50MiB 的参考/底图像素。
func RejectGenerationInput(parts [][]byte) error {
	if SumBytes(parts) > GenerationMaxBatchBytes {
		return apperr.Validationf("发给供应商的图片总大小超过限制: %s", FormatByteSize(GenerationMaxBatchBytes))
	}
	return nil
}

// RejectGenerationOutput 在整批落盘前校验：声明 MIME（非空时）须在 DefaultLimits 允许集，
// 单张 ≤10MiB，合计 ≤50MiB。任一张失败则整批拒绝，调用方不得部分写入。
func RejectGenerationOutput(images [][]byte, declaredMIME string) error {
	if mime := NormalizeMIME(declaredMIME); mime != "" {
		if !DefaultLimits().Allows(mime) {
			return apperr.Validationf("供应商返回的图片格式不受支持: %s", mime)
		}
	}
	total := 0
	for _, img := range images {
		n := len(img)
		if n > GenerationMaxImageBytes {
			return apperr.Validationf("供应商返回的图片超过单张大小限制: %s", FormatByteSize(GenerationMaxImageBytes))
		}
		total += n
		if total > GenerationMaxBatchBytes {
			return apperr.Validationf("供应商返回的图片总大小超过限制: %s", FormatByteSize(GenerationMaxBatchBytes))
		}
	}
	return nil
}
