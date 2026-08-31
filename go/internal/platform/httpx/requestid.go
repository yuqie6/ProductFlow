package httpx

import (
	"crypto/rand"
	"encoding/hex"
)

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败时仍要回 x-request-id，避免把错误暴露给客户端。
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf[:])
}
