package httpx

import (
	"crypto/rand"
	"encoding/hex"
)

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf[:])
}
