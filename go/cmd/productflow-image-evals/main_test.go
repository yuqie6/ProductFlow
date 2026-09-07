package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/imageeval"
)

func TestRunRejectsMalformedInputFlagsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"--product-inputs"}, {"--n", "invalid"}, {"--seed", "invalid"}, {"unexpected"}} {
		if got := runLive(args); got != 2 {
			t.Fatalf("args=%v exit=%d", args, got)
		}
	}
}

func TestPrepareRequiresOutputBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{nil, {"--out"}, {"--out", ""}, {"--out", "unused.json", "--n", "invalid"}} {
		if got := runPrepareInputs(args); got != 2 {
			t.Fatalf("args=%v exit=%d", args, got)
		}
	}
}

func TestPrepareAnnotationsRejectsInvalidArgumentsBeforePoolRead(t *testing.T) {
	for _, args := range [][]string{nil, {"--out"}, {"--out", ""}, {"--out", "unused.json", "--types", "invalid"}} {
		if got := runPrepareAnnotations(args); got != 2 {
			t.Fatalf("args=%v exit=%d", args, got)
		}
	}
}

func TestAnnotateRejectsMissingArgumentsBeforeProviderResolution(t *testing.T) {
	for _, args := range [][]string{nil, {"--selection"}, {"--out", "unused"}, {"--selection", "selection.json", "--out"}} {
		if got := runAnnotate(args); got != 2 {
			t.Fatalf("args=%v exit=%d", args, got)
		}
	}
}

func TestAnnotateRefusesExistingOutputBeforeProviderResolution(t *testing.T) {
	root := t.TempDir()
	selectionPath := filepath.Join(root, "selection.json")
	poolPath := filepath.Join(root, imageeval.StorageDir, imageeval.PoolDir)
	selection := imageeval.AnnotationSelection{
		SchemaVersion: imageeval.AnnotationSelectionSchemaVersion, ContractVersion: imageeval.AnnotationContractVersion,
		Mode: imageeval.AnnotationModeReference, PoolPath: poolPath, PoolIndexSHA256: strings.Repeat("a", 64),
		CreatedAt: time.Now().UTC(), Commit: "test-commit", N: 1,
		Cases: []imageeval.AnnotationCaseSelection{{
			CaseID: "case-1", Category: "home", Title: "test", ManifestSHA256: strings.Repeat("b", 64), ImageTypes: []string{"hero"},
			IdentityReferences: []imageeval.AnnotationAsset{{AssetID: "reference:0", Path: "references/0.png", SHA256: strings.Repeat("c", 64), Width: 1, Height: 1, ImageType: "sku", Role: "identity_reference", Source: "manifest.references"}},
			QualityReferences:  []imageeval.AnnotationAsset{{AssetID: "quality:hero", Path: "gold/hero.png", SHA256: strings.Repeat("d", 64), Width: 1, Height: 1, ImageType: "hero", Role: "quality_reference", Source: "manifest.gold"}},
		}},
	}
	raw, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectionPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "existing-output")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(imageeval.RunSwitch, "1")
	t.Setenv("DATABASE_URL", "invalid-dsn-for-preflight-test")
	if got := runAnnotate([]string{"--selection", selectionPath, "--out", out}); got != 1 {
		t.Fatalf("existing output must fail during reservation, exit=%d", got)
	}
	if _, err := os.Stat(filepath.Join(out, ".annotation-reservation.json")); !os.IsNotExist(err) {
		t.Fatalf("existing output should not be modified during preflight: %v", err)
	}
}
