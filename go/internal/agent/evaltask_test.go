package agent

import (
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
