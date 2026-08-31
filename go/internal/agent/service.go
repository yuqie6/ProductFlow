// Package agent 实现 Agent Session、Task、Conversation、Turn 投影、内部工具面与执行 lease。
//
// 职责：Session 是长期对话容器；Task 是用户显式 Goal；Conversation 挂 Turn；Turn 投影与
// agent_turn_events journal 是浏览器对话的权威。Pi session files 只给模型 loop 用，不能证明
// 恢复、对账或多实例。改图必须调用 graph 包，禁止直接写 workflow_graphs。
//
// 调用时机：Web 走 Register 的 /api/v2/agent-*；agent-service 走内部 claim / events / lease。
// dispatcher 崩溃恢复走 [RecoveryService] / RecoverUnfinishedTurns。
//
// 副作用：写 agent_sessions、agent_tasks、agent_conversations、agent_turn_projections、
// agent_turn_executions、agent_turn_events、checkpoint。GraphRun 请求复用 graph.SubmitRun。
//
// 错误：lease/fencing 失败是 Conflict；序号打架是 CodeEventSequenceConflict。
// 产品 Goal 不会因 Turn 或 WorkflowGraphRun 成功自动完成。waiting_reason=goal_loop 时
// 读路径（含 SyncGraphRunToTasks）不得覆盖用户拥有的状态。
//
// 约束：SSE 只从 PostgreSQL 游标回放，不要给 agent-service 再开本地事件流。
package agent

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

// Service 拥有 Agent Session / Task / Conversation / Turn 投影与内部工具面。
// DB 是命令路径；Pool 给 LISTEN/NOTIFY 与部分只读。Gateway 空则不能提交模型 Turn。
// Poll 是 SSE/同步等待间隔。改 graph 表必须走 Graph，不要在本结构上开 SQL。
type Service struct {
	DB       *gorm.DB        // 命令路径 GORM
	Pool     *pgxpool.Pool   // LISTEN/NOTIFY 与部分只读
	Graph    graph.Service   // 改图必须走这里
	Product  product.Service // 商品身份与图库
	Library  library.Service // 全局图库整理 Draft
	Media    media.Store     // 读图 bytes
	Settings *settings.Store // 运行时开关与供应商配置
	Gateway  Gateway         // 空则不能提交模型 Turn
	Poll     time.Duration   // SSE/同步等待间隔，来自 AGENT_TURN_SYNC_POLL_SECONDS
	control  *controlHub
}

// RecoveryService 构造 dispatcher 崩溃恢复用的 Service：带 Graph/Product，才能对账并原键重试副作用。
// 不要拿这个实例去挂 HTTP；它没有 Gateway / Library / Settings。
func RecoveryService(pool *pgxpool.Pool, gdb *gorm.DB) Service {
	return Service{
		DB:   gdb,
		Pool: pool,
		Graph: graph.Service{
			DB: gdb, Pool: pool, AfterRunStatus: SyncGraphRunToTasks,
			AfterProposalDecision: SyncGraphProposalDecision, Products: product.GraphGuard{},
		},
		Product: product.Service{DB: gdb, Canvas: WriteProductCanvas},
	}
}

// GatewayConfigured 报告 Gateway 是否已配置；未实现 Configured 的实现视为已配置。
func (s Service) GatewayConfigured() bool {
	if s.Gateway == nil {
		return false
	}
	type configured interface{ Configured() bool }
	if g, ok := s.Gateway.(configured); ok {
		return g.Configured()
	}
	return true
}
