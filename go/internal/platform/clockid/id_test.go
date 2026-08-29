package clockid

import "testing"

func TestNewIsCanonicalUUID(t *testing.T) {
	id := New()
	got, err := Normalize(id)
	if err != nil {
		t.Fatal(err)
	}
	if got != id || len(id) != 36 {
		t.Fatalf("id %q", id)
	}
}

func TestNormalizeAcceptsBareHex(t *testing.T) {
	got, err := Normalize("11111111111141118111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if got != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("got %q", got)
	}
}
