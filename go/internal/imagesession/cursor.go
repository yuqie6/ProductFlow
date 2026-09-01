package imagesession

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const (
	imageSessionListDefaultLimit = 20
	imageSessionListMaxLimit     = 100
	imageSessionCursorVersion    = 1
)

type imageSessionListCursor struct {
	Version   int    `json:"v"`
	UpdatedAt string `json:"updated_at"`
	ID        string `json:"id"`
}

func encodeImageSessionListCursor(updatedAt time.Time, id string) (string, error) {
	payload, err := json.Marshal(imageSessionListCursor{
		Version:   imageSessionCursorVersion,
		UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano),
		ID:        id,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeImageSessionListCursor(raw string) (imageSessionListCursor, time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return imageSessionListCursor{}, time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return imageSessionListCursor{}, time.Time{}, false
	}
	var cursor imageSessionListCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != imageSessionCursorVersion || cursor.ID == "" {
		return imageSessionListCursor{}, time.Time{}, false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, cursor.UpdatedAt)
	if err != nil {
		return imageSessionListCursor{}, time.Time{}, false
	}
	return cursor, updatedAt.UTC(), true
}
