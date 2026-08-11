package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkillReadsInstructionsAndContainedResources(t *testing.T) {
	workspace := t.TempDir()
	skillPath := writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "review-code", "Review code safely")
	referenceDir := filepath.Join(filepath.Dir(skillPath), "references")
	if err := os.Mkdir(referenceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(referenceDir, "guide.md"), []byte("review guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := Discover(Options{WorkingDir: workspace, UserHome: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tool := catalog.Tool()
	skillPath, _ = filepath.Abs(skillPath)

	instructions := json.RawMessage(`{"name":"review-code"}`)
	if !tool.CanAutoApprove(instructions) {
		t.Fatal("discovered SKILL.md was not auto-approved")
	}
	output, err := tool.Handler(context.Background(), instructions)
	if err != nil || !strings.Contains(output, "Follow this workflow") || !strings.Contains(output, `"name":"review-code"`) {
		t.Fatalf("output = %q, err = %v", output, err)
	}

	resource := json.RawMessage(`{"name":"review-code","resource_path":"references/guide.md"}`)
	if !tool.CanAutoApprove(resource) {
		t.Fatal("contained resource was not auto-approved")
	}
	output, err = tool.Handler(context.Background(), resource)
	if err != nil || !strings.Contains(output, "review guide") {
		t.Fatalf("resource output = %q, err = %v", output, err)
	}
}

func TestLoadSkillRejectsUnknownAndEscapingResources(t *testing.T) {
	workspace := t.TempDir()
	skillPath := writeSkill(t, filepath.Join(workspace, ".agents", "skills"), "safe-skill", "Safe workflow")
	catalog, err := Discover(Options{WorkingDir: workspace, UserHome: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tool := catalog.Tool()
	skillPath, _ = filepath.Abs(skillPath)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"name":"safe-skill","skill_path":` + quoted(outside) + `}`),
		json.RawMessage(`{"name":"safe-skill","resource_path":"../outside.md"}`),
	} {
		if tool.CanAutoApprove(raw) {
			t.Fatalf("unsafe request auto-approved: %s", raw)
		}
		if _, err := tool.Handler(context.Background(), raw); err == nil {
			t.Fatalf("unsafe request executed: %s", raw)
		}
	}

	link := filepath.Join(filepath.Dir(skillPath), "references", "outside.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err == nil {
		raw := json.RawMessage(`{"name":"safe-skill","resource_path":"references/outside.md"}`)
		if tool.CanAutoApprove(raw) {
			t.Fatal("escaping symlink resource auto-approved")
		}
	}
}

func TestLoadSkillRequiresPathToDisambiguateDuplicateNames(t *testing.T) {
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workingDir := filepath.Join(repository, "service")
	if err := os.Mkdir(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(repository, ".agents", "skills"), "shared", "Repository workflow")
	selectedPath := writeSkill(t, filepath.Join(workingDir, ".agents", "skills"), "shared", "Service workflow")
	catalog, err := Discover(Options{WorkingDir: workingDir, UserHome: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	tool := catalog.Tool()

	ambiguous := json.RawMessage(`{"name":"shared"}`)
	if tool.CanAutoApprove(ambiguous) {
		t.Fatal("ambiguous Skill name was auto-approved")
	}
	if _, err := tool.Handler(context.Background(), ambiguous); err == nil || !strings.Contains(err.Error(), "同名") {
		t.Fatalf("ambiguous load error = %v", err)
	}

	selectedPath, _ = filepath.Abs(selectedPath)
	disambiguated := json.RawMessage(`{"name":"shared","skill_path":` + quoted(selectedPath) + `}`)
	if !tool.CanAutoApprove(disambiguated) {
		t.Fatal("catalog path did not disambiguate Skill")
	}
	output, err := tool.Handler(context.Background(), disambiguated)
	if err != nil || !strings.Contains(output, "Service workflow") {
		t.Fatalf("output = %q, err = %v", output, err)
	}
}

func quoted(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
