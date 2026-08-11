package durable_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuqie6/agent-harness/durable"
)

func TestPublicPackageCanDriveRecoverableEdit(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "public.txt")
	before := []byte("public before\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	tool, err := durable.NewFileEditTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := durable.Open(filepath.Join(t.TempDir(), "jobs.db"), durable.Options{}, tool)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	digest := sha256.Sum256(before)
	input, _ := json.Marshal(durable.FileEditInput{
		Path: "public.txt", ExpectedSHA256: hex.EncodeToString(digest[:]),
		OldText: "before", NewText: "after",
	})
	job, err := engine.Submit(t.Context(), durable.JobSpec{
		Name: "external consumer", Steps: []durable.StepSpec{{Tool: tool.Name(), Input: input}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err = engine.Run(t.Context(), job.ID)
	if err != nil || job.Status != durable.JobSucceeded {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
}
