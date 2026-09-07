package auth

import (
	"strings"
	"testing"
)

func TestValidatePasswordUsesBcryptByteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "72 ascii bytes", password: strings.Repeat("a", 72)},
		{name: "72 utf8 bytes", password: strings.Repeat("界", 24)},
		{name: "73 ascii bytes", password: strings.Repeat("a", 73), wantErr: true},
		{name: "75 utf8 bytes", password: strings.Repeat("界", 25), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePassword(tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validatePassword bytes=%d err=%v wantErr=%v", len([]byte(tc.password)), err, tc.wantErr)
			}
		})
	}
}
