package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEvalTasksIncludesL2Contract(t *testing.T) {
	root := DefaultEvalRoot()
	if root == "" {
		t.Fatal("eval root")
	}
	tasks, worlds, err := LoadEvalTasks(root, "l2")
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) < 4 {
		t.Fatalf("worlds %d", len(worlds))
	}
	if len(tasks) < 15 {
		t.Fatalf("L2 tasks %d, want >= 15", len(tasks))
	}
	perSkill := map[string]int{}
	for _, task := range tasks {
		if task.Expect.State == nil {
			t.Fatalf("L2 task %s missing expect.state", task.ID)
		}
		perSkill[task.Skill]++
		if _, ok := worlds[task.World]; !ok {
			t.Fatalf("task %s world %s", task.ID, task.World)
		}
	}
	if len(perSkill) < 5 {
		t.Fatalf("L2 skills %+v", perSkill)
	}
	for skill, count := range perSkill {
		if count < 3 {
			t.Fatalf("skill %s has %d L2 tasks, want >= 3", skill, count)
		}
	}
	filtered := FilterEvalTasks(tasks, "graph-editing")
	if len(filtered) == 0 {
		t.Fatal("filter graph-editing returned no L2 tasks")
	}
	for _, task := range filtered {
		if task.Skill != "graph-editing" && !strings.Contains(task.ID, "graph-editing") {
			t.Fatalf("filter leaked %s", task.ID)
		}
	}
}

func TestLoadEvalTasksRejectsInvalidSharedFixtures(t *testing.T) {
	fixtureRoot := filepath.Join(DefaultEvalRoot(), "fixtures", "invalid")
	cases := []struct {
		name   string
		worlds []string
		tasks  []string
		want   string
	}{
		{
			name:   "extra field",
			worlds: []string{"world.json"},
			tasks:  []string{"extra-field-task.json"},
			want:   `unknown field "unexpected_field"`,
		},
		{
			name:   "extra page_context field",
			worlds: []string{"world.json"},
			tasks:  []string{"extra-field-page-context-task.json"},
			want:   `unknown field "unexpected_field"`,
		},
		{
			name:   "extra world field",
			worlds: []string{"extra-field-world.json"},
			tasks:  []string{"unknown-world-task.json"},
			want:   `unknown field "unexpected_field"`,
		},
		{
			name:   "unknown enum",
			worlds: []string{"world.json"},
			tasks:  []string{"unknown-enum-task.json"},
			want:   `invalid suite "nightly"`,
		},
		{
			name:   "duplicate id",
			worlds: []string{"world.json"},
			tasks:  []string{"duplicate-id-a.json", "duplicate-id-b.json"},
			want:   "duplicate eval task id fixture-duplicate-id",
		},
		{
			name:   "unknown world",
			worlds: []string{"world.json"},
			tasks:  []string{"unknown-world-task.json"},
			want:   "references unknown world does-not-exist",
		},
		{
			name:   "layer mismatch",
			worlds: []string{"world.json"},
			tasks:  []string{"layer-mismatch-task.json"},
			want:   "layer l2 requires expect.state",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := installEvalFixtures(t, fixtureRoot, tc.worlds, tc.tasks)
			_, _, err := LoadEvalTasks(root, "")
			if err == nil {
				t.Fatal("expected load error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q, want substring %q", err, tc.want)
			}
		})
	}
}

func installEvalFixtures(t *testing.T, fixtureRoot string, worlds, tasks []string) string {
	t.Helper()
	root := t.TempDir()
	copyNamedJSON(t, filepath.Join(root, "worlds"), fixtureRoot, worlds)
	copyNamedJSON(t, filepath.Join(root, "tasks"), fixtureRoot, tasks)
	return root
}

func copyNamedJSON(t *testing.T, destDir, srcDir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destDir, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
