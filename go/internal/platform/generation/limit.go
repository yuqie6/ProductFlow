// Package generation 提供图运行与连续生图共用的并发上限解析和只读容量快照。
// 真正的 claim 仍只发生在 worker（graph.claimQueuedNodeRun / imagesession.Executor.claim）。
package generation

import (
	"context"
	"errors"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const (
	MaxConcurrentSettingKey = "generation_max_concurrent_tasks"
	DefaultMaxConcurrent    = 3
	MinMaxConcurrent        = 1
	MaxMaxConcurrent        = 20
)

// ParseMaxConcurrent 解析 generation_max_concurrent_tasks。
// 空值或非正整数回落到 3；结果夹在 1–20。只认无符号十进制数字，不接受符号、空白或小数。
func ParseMaxConcurrent(raw string) int {
	n := DefaultMaxConcurrent
	if raw != "" {
		parsed := 0
		for _, ch := range raw {
			if ch < '0' || ch > '9' {
				parsed = 0
				break
			}
			parsed = parsed*10 + int(ch-'0')
		}
		if parsed > 0 {
			n = parsed
		}
	}
	if n < MinMaxConcurrent {
		n = MinMaxConcurrent
	}
	if n > MaxMaxConcurrent {
		n = MaxMaxConcurrent
	}
	return n
}

// LoadMaxConcurrent 读 app_settings.generation_max_concurrent_tasks 再 ParseMaxConcurrent。
// 键缺失当空值。其它数据库错误原样返回，此时 int 为默认 3。
func LoadMaxConcurrent(ctx context.Context, db *gorm.DB) (int, error) {
	var rec schema.AppSettings
	err := db.WithContext(ctx).Where("key = ?", MaxConcurrentSettingKey).Take(&rec).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ParseMaxConcurrent(""), err
	}
	return ParseMaxConcurrent(rec.Value), nil
}
