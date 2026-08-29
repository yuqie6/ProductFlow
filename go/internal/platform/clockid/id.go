package clockid

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// New returns a random UUID v4 string (36 chars, hyphenated).
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

// Normalize accepts hyphenated or 32-hex UUIDs and returns the canonical 36-char form.
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
