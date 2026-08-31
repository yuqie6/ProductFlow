// Package clockid 生成带连字符的 UUID v4 主键。名字像时间有序，实现是随机 v4，不要按时间排序依赖它。
package clockid

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// New 返回 36 字符 hyphenated UUID v4。crypto/rand 失败会 panic：主键不能静默退化成全零。
func New() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return format(buf)
}

func format(buf [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// Normalize 接受带连字符或 32 位 hex，收成小写 8-4-4-4-12。长度或字符非法返回「媒体 ID 必须是 UUID」。
func Normalize(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return "", fmt.Errorf("媒体 ID 必须是 UUID")
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("媒体 ID 必须是 UUID")
		}
	}
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32], nil
}
