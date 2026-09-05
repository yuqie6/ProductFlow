package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestAgentBrowserGapCapacity(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_AGENT_BROWSER_GAP") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_AGENT_BROWSER_GAP=1; requires installed Web dependencies and Chromium")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("browser gap gate requires DATABASE_URL")
	}
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_agap_%d", time.Now().UnixNano()))
	as := newAgentServerOnDB(t, mockGateway{}, "", pool, db)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cases := []map[string]any{}
	for _, count := range []int{3, 10000} {
		claimed := createClaimedJournalTurn(t, as)
		if err := appendCapacityJournalTurn(ctx, as, claimed, count, &appendLatencyRecorder{}); err != nil {
			t.Fatal(err)
		}
		if err := assertCapacityJournalTurn(ctx, as, claimed, count); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, map[string]any{
			"count": count, "runId": claimed.turn.HarnessRunID, "turnId": claimed.turn.HarnessTurnID,
			"url": "/api/v2/agent-conversations/" + claimed.conversationID + "/turns/" + claimed.turn.ID + "/events",
		})
	}
	cookies := []map[string]string{}
	for _, cookie := range as.cookies {
		cookies = append(cookies, map[string]string{"name": cookie.Name, "value": cookie.Value})
	}
	input, err := json.Marshal(map[string]any{"api": as.srv.URL, "cookies": cookies, "cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	web, err := filepath.Abs("../../../web")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", "scripts/agent-gap-capacity.mjs")
	cmd.Dir = web
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	t.Logf("Chromium gap gate:\n%s", output)
	if err != nil {
		t.Fatalf("browser gap gate: %v", err)
	}
}
