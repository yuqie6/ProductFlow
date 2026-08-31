package metrics

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

var AgentSSEConnections atomic.Int64

type statusCount struct {
	Status string `gorm:"column:status"`
	Count  int64  `gorm:"column:count"`
}

// Register keeps metrics private by default: no token means no route.
func Register(engine *gin.Engine, db *gorm.DB, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	engine.GET("/metrics", func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			c.Status(http.StatusUnauthorized)
			return
		}
		provided := strings.TrimPrefix(authorization, "Bearer ")
		if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			c.Status(http.StatusUnauthorized)
			return
		}
		body, err := snapshot(db.WithContext(c.Request.Context()))
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(body))
	})
}

func snapshot(db *gorm.DB) (string, error) {
	var turns, runs, dispatches, invocations, reconciliations []statusCount
	queries := []struct {
		model any
		out   *[]statusCount
	}{
		{&schema.AgentTurnProjections{}, &turns},
		{&schema.WorkflowGraphRuns{}, &runs},
		{&schema.AsyncDispatches{}, &dispatches},
		{&schema.AgentModelInvocations{}, &invocations},
	}
	if err := db.Model(&schema.AgentTurnEffectReconciliations{}).
		Select("reconciliation_state AS status, COUNT(*) AS count").Group("reconciliation_state").Scan(&reconciliations).Error; err != nil {
		return "", err
	}
	var tokenTotals struct {
		InputTokens  int64 `gorm:"column:input_tokens"`
		OutputTokens int64 `gorm:"column:output_tokens"`
		UsageMissing int64 `gorm:"column:usage_missing"`
	}
	if err := db.Model(&schema.AgentModelInvocations{}).Select(`
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COUNT(*) FILTER (WHERE usage_source = 'unavailable') AS usage_missing`).Scan(&tokenTotals).Error; err != nil {
		return "", err
	}
	for _, query := range queries {
		if err := db.Model(query.model).Select("status, COUNT(*) AS count").Group("status").Scan(query.out).Error; err != nil {
			return "", err
		}
	}
	var b strings.Builder
	b.WriteString("# HELP productflow_agent_sse_connections Current Agent SSE connections.\n")
	b.WriteString("# TYPE productflow_agent_sse_connections gauge\n")
	fmt.Fprintf(&b, "productflow_agent_sse_connections %d\n", AgentSSEConnections.Load())
	writeStatusCounts(&b, "productflow_agent_turns", "Agent Turns by durable status.", turns)
	writeStatusCounts(&b, "productflow_graph_runs", "Workflow runs by durable status.", runs)
	writeStatusCounts(&b, "productflow_async_dispatches", "Async dispatch records by durable status.", dispatches)
	writeStatusCounts(&b, "productflow_agent_model_invocations", "Model invocations by durable status.", invocations)
	writeStatusCounts(&b, "productflow_agent_effect_reconciliations", "Effect reconciliations by bounded state.", reconciliations)
	b.WriteString("# TYPE productflow_agent_model_input_tokens_observed gauge\n")
	fmt.Fprintf(&b, "productflow_agent_model_input_tokens_observed %d\n", tokenTotals.InputTokens)
	b.WriteString("# TYPE productflow_agent_model_output_tokens_observed gauge\n")
	fmt.Fprintf(&b, "productflow_agent_model_output_tokens_observed %d\n", tokenTotals.OutputTokens)
	b.WriteString("# TYPE productflow_agent_model_usage_missing gauge\n")
	fmt.Fprintf(&b, "productflow_agent_model_usage_missing %d\n", tokenTotals.UsageMissing)
	return b.String(), nil
}

func writeStatusCounts(b *strings.Builder, name, help string, rows []statusCount) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	for _, row := range rows {
		if safeLabel(row.Status) {
			fmt.Fprintf(b, "%s{status=%q} %d\n", name, row.Status, row.Count)
		}
	}
}

func safeLabel(value string) bool {
	if value == "" || len(value) > 40 {
		return false
	}
	for _, ch := range value {
		if (ch < 'a' || ch > 'z') && ch != '_' {
			return false
		}
	}
	return true
}
