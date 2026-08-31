package recipe

import (
	"strings"
	"testing"
)

func TestRewriteImagePromptPayloadUpdatesCanonicalHash(t *testing.T) {
	raw := []byte(`{"schema_version":3,"nodes":[{"key":"prompt","node_type":"prompt_generation","title":"prompt_generation remains in titles","position_x":0,"position_y":0,"group_key":null,"config":{"image_type_key":"hero"}}],"edges":[],"groups":[]}`)

	encoded, hash, changed, err := rewriteImagePromptPayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected legacy node type to be rewritten")
	}
	if strings.Contains(string(encoded), `"node_type":"prompt_generation"`) || !strings.Contains(string(encoded), `"node_type":"image_prompt"`) {
		t.Fatalf("unexpected payload %s", encoded)
	}
	if !strings.Contains(string(encoded), `"title":"prompt_generation remains in titles"`) {
		t.Fatalf("migration changed a non-contract string: %s", encoded)
	}
	if _, err := parsePayloadOrRaise(encoded, hash); err != nil {
		t.Fatalf("rewritten payload/hash cannot be read: %v", err)
	}

	again, _, changed, err := rewriteImagePromptPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if changed || string(again) != string(encoded) {
		t.Fatalf("migration is not idempotent: changed=%v payload=%s", changed, again)
	}
}
