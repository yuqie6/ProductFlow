package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverFindsNestedRepositoryAndUserSkills(t *testing.T) {
	repository := t.TempDir()
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workingDir := filepath.Join(repository, "services", "api")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userHome := t.TempDir()
	writeSkill(t, filepath.Join(repository, ".agents", "skills"), "root-skill", "Root workflow")
	writeSkill(t, filepath.Join(workingDir, ".agents", "skills"), "local-skill", "Local workflow")
	writeSkill(t, filepath.Join(userHome, ".agents", "skills"), "user-skill", "User workflow")
	writeSkill(t, filepath.Join(userHome, ".codex", "skills"), "codex-skill", "Codex-compatible workflow")
	writeSkill(t, filepath.Join(repository, ".agents", "skills"), "shared", "Root shared")
	writeSkill(t, filepath.Join(workingDir, ".agents", "skills"), "shared", "Local shared")
	badDir := filepath.Join(repository, ".agents", "skills", "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("not frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := Discover(Options{WorkingDir: workingDir, UserHome: userHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills()) != 6 {
		t.Fatalf("skills = %#v", catalog.Skills())
	}
	counts := map[string]int{}
	scopes := map[string]Scope{}
	for _, skill := range catalog.Skills() {
		counts[skill.Name]++
		scopes[skill.Name] = skill.Scope
	}
	if counts["shared"] != 2 || counts["root-skill"] != 1 || counts["local-skill"] != 1 || counts["user-skill"] != 1 || counts["codex-skill"] != 1 {
		t.Fatalf("counts = %#v", counts)
	}
	if scopes["user-skill"] != ScopeUser || scopes["local-skill"] != ScopeRepository {
		t.Fatalf("scopes = %#v", scopes)
	}
	if warnings := catalog.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "缺少起始") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestDiscoverParsesYAMLAndValidatesStandardMetadata(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, ".agents", "skills")
	directory := filepath.Join(root, "yaml-skill")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: yaml-skill\ndescription: >\n  Diagnose failures and\n  verify the fix.\nmetadata:\n  owner: test\n---\n# Instructions\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, root, "wrong-directory", "Valid description")
	wrong := filepath.Join(root, "wrong-directory", "SKILL.md")
	data, err := os.ReadFile(wrong)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "name: wrong-directory", "name: another-name", 1))
	if err := os.WriteFile(wrong, data, 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := Discover(Options{WorkingDir: workspace, UserHome: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills()) != 1 || catalog.Skills()[0].Description != "Diagnose failures and verify the fix." {
		t.Fatalf("skills = %#v", catalog.Skills())
	}
	if warnings := catalog.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "与父目录") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func writeSkill(t *testing.T, root, name, description string) string {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "SKILL.md")
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\nFollow this workflow.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
