package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

// Eval observation hosts use one explicit prefix so their one-shot databases are
// attributable to the scope-observation task. Individual library fixture tests
// keep their own helper and prefix.
func newEvalHostServer(t *testing.T) *agentServer {
	t.Helper()
	prefix := strings.TrimSpace(os.Getenv("PRODUCTFLOW_EVAL_HOST_DB_PREFIX"))
	if prefix == "" {
		prefix = "eval_scope"
	}
	pool, db := testdb.IsolatedMigrated(t, prefix+"_"+strings.ReplaceAll(clockid.New(), "-", ""))
	return newAgentServerOnDB(t, mockGateway{}, "tok", pool, db)
}
