package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

// Intake observation hosts use an explicit prefix so their one-shot databases are
// attributable to this eval task. Existing graph/library hosts retain their names.
func newEvalHostServer(t *testing.T) *agentServer {
	t.Helper()
	prefix := strings.TrimSpace(os.Getenv("PRODUCTFLOW_EVAL_HOST_DB_PREFIX"))
	if prefix == "" {
		return newEvalLibraryServer(t)
	}
	pool, db := testdb.IsolatedMigrated(t, prefix+"_"+strings.ReplaceAll(clockid.New(), "-", ""))
	return newAgentServerOnDB(t, mockGateway{}, "tok", pool, db)
}
