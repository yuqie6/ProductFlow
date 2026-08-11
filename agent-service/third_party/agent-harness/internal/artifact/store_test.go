package artifact

import (
	"os"
	"strings"
	"testing"
)

func TestPackPersistsLargeOutputAndPagesIt(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("head\n", 100) + strings.Repeat("tail\n", 100)
	packed, reference, err := store.Pack(full, 128)
	if err != nil {
		t.Fatal(err)
	}
	if reference == nil || reference.Bytes != len(full) || packed == full {
		t.Fatalf("reference = %#v, packed bytes = %d", reference, len(packed))
	}
	envelope, ok := ParseEnvelope(packed)
	if !ok || envelope.Artifact != *reference || !strings.Contains(envelope.Preview, "artifact 中省略内容") {
		t.Fatalf("envelope = %#v, ok = %v", envelope, ok)
	}
	loaded, err := store.Load(*reference)
	if err != nil || loaded != full {
		t.Fatalf("loaded bytes = %d, err = %v", len(loaded), err)
	}
	page, err := store.ReadPage(reference.ID, 5, 20)
	if err != nil || page.Content != full[5:25] || page.NextOffset != 25 || page.TotalBytes != int64(len(full)) {
		t.Fatalf("page = %#v, err = %v", page, err)
	}
	info, err := os.Stat(store.path(reference.ID))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact mode = %v, err = %v", info.Mode().Perm(), err)
	}
}

func TestPackLeavesSmallOutputInline(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packed, reference, err := store.Pack("small", 128)
	if err != nil || packed != "small" || reference != nil {
		t.Fatalf("packed = %q, reference = %#v, err = %v", packed, reference, err)
	}
}

func TestPackKeepsOutputInlineWhenExternalizationIsDisabled(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	output := strings.Repeat("large", 1000)
	packed, reference, err := store.Pack(output, 0)
	if err != nil || packed != output || reference != nil {
		t.Fatalf("packed bytes = %d, reference = %#v, err = %v", len(packed), reference, err)
	}
}

func TestPackedEnvelopeStaysBoundedForEscapedOutput(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const limit = 256
	packed, reference, err := store.Pack(strings.Repeat("\x00", 2000), limit)
	if err != nil {
		t.Fatal(err)
	}
	if reference == nil || len(packed) > limit+1024 {
		t.Fatalf("reference = %#v, packed bytes = %d", reference, len(packed))
	}
}

func TestReadPageRejectsInvalidReference(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadPage("../../secret", 0, 10); err == nil {
		t.Fatal("invalid artifact ID accepted")
	}
}
