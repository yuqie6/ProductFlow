package providers

import "encoding/base64"

func decodeStdB64(raw string) ([]byte, error) {
	out, err := base64.StdEncoding.DecodeString(raw)
	if err == nil {
		return out, nil
	}
	return base64.RawStdEncoding.DecodeString(raw)
}
