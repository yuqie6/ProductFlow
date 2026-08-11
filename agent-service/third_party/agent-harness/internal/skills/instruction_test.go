package skills

import (
	"fmt"
	"strings"
	"testing"
)

func TestInstructionUsesProgressiveDisclosureBudget(t *testing.T) {
	catalog := &Catalog{}
	for i := 0; i < 20; i++ {
		catalog.skills = append(catalog.skills, Skill{
			Name: fmt.Sprintf("skill-%02d", i), Description: strings.Repeat("detailed trigger ", 30),
			Path: fmt.Sprintf("/workspace/.agents/skills/skill-%02d/SKILL.md", i),
		})
	}
	instruction := catalog.Instruction(600)
	if instruction == "" || len(instruction) > 600 {
		t.Fatalf("instruction bytes = %d: %q", len(instruction), instruction)
	}
	if !strings.Contains(instruction, `"name":"skill-00"`) || strings.Contains(instruction, "Follow this workflow") {
		t.Fatalf("instruction = %q", instruction)
	}
	if !strings.Contains(instruction, "元数据预算") {
		t.Fatalf("omission warning missing: %q", instruction)
	}
}

func TestInstructionIsEmptyWithoutSkillsOrUsableBudget(t *testing.T) {
	if got := (&Catalog{}).Instruction(24000); got != "" {
		t.Fatalf("empty catalog instruction = %q", got)
	}
	catalog := &Catalog{skills: []Skill{{Name: "one", Description: "one", Path: "/one/SKILL.md"}}}
	if got := catalog.Instruction(100); got != "" {
		t.Fatalf("tiny budget instruction = %q", got)
	}
}
