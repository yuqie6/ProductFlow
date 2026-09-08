package schema_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestApplyAddsWorkflowRequestUnknownToExistingEnum(t *testing.T) {
	_, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_request_enum_%d", time.Now().UnixNano()))
	for _, sql := range []string{
		"ALTER TABLE agent_workflow_run_requests ALTER COLUMN status TYPE text",
		"DROP TYPE agentworkflowrunrequeststatus",
		"CREATE TYPE agentworkflowrunrequeststatus AS ENUM ('awaiting_confirmation','confirmed','succeeded','failed','cancelled')",
		"ALTER TABLE agent_workflow_run_requests ALTER COLUMN status TYPE agentworkflowrunrequeststatus USING status::agentworkflowrunrequeststatus",
		"CREATE TABLE request_enum_probe (status agentworkflowrunrequeststatus NOT NULL)",
		"INSERT INTO request_enum_probe VALUES ('confirmed')",
	} {
		if err := gdb.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := schema.Apply(gdb); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	if err := gdb.Raw("SELECT status FROM request_enum_probe").Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "confirmed" {
		t.Fatalf("existing status changed: %s", status)
	}
	if err := gdb.Exec("UPDATE request_enum_probe SET status='unknown'").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Raw("SELECT status FROM request_enum_probe").Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "unknown" {
		t.Fatalf("new value unusable: %s", status)
	}
}
