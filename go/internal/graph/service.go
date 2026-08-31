package graph

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Service 是 schema-v3 图的应用入口：空画布、ChangeSet、提案、文稿候选与 GraphRun。
// 改图走 Mutate（只 flush）；跑图走 SubmitRun 写 PENDING dispatch，不在请求里打 broker。
// 必须注入 ProductGuard。本包不得 import product。
type Service struct {
	DB   *gorm.DB      // 命令事务入口；改图与提交运行都走 tx.WithGorm
	Pool *pgxpool.Pool // SSE LISTEN 用；recovery 走独立入口
	// AfterRunStatus 在 CancelRunTx 取消成功后、同一事务里回调，供 Agent 同步 Task。
	// nil 跳过。失败回滚这次取消。ExecuteRun 终态走 Executor.AfterRunStatus，不走这里。
	AfterRunStatus func(ctx context.Context, tx *gorm.DB, runID string) error
	// AfterProposalDecision 在确认或丢弃提案成功后、同一事务里回调，供 Agent journal 同步。
	// nil 跳过。失败回滚这次确认/丢弃。
	AfterProposalDecision func(ctx context.Context, tx *gorm.DB, productID, graphID, proposalID, decision string) error
	// Products 经 guardCtx 挂到改图/投影/跑图提交的 ctx 上。必须注入；nil 时需要守卫的路径返回 Internal。
	Products ProductGuard
}

func (s Service) guardCtx(ctx context.Context) context.Context {
	return WithProductGuard(ctx, s.Products)
}

// CreateEmpty 为商品持久化一张空的 active schema-v3 图。商品已有 active 图时返回 Conflict。
func (s Service) CreateEmpty(ctx context.Context, productID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := CreateEmpty(ctx, pgxTx, productID, DefaultGraphTitle)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// Current 读取商品当前 active 图。没有 active 图时返回 NotFound，不返回 nil Projection。
func (s Service) Current(ctx context.Context, productID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// Get 按商品与图 id 读取 live 图。图不属于该商品时返回 NotFound。
func (s Service) Get(ctx context.Context, productID, graphID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// ApplyChangeSet 以 ActorUser 写入一条可逆 ChangeSet。base_graph_revision 不匹配时返回 Conflict。
func (s Service) ApplyChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet) (Projection, error) {
	return s.applyChangeSet(ctx, productID, graphID, changeSet, ActorUser)
}

// ApplyAgentChangeSet 立即写入一条 Agent 可逆命令；actor 固定为 agent。多步改图须走提案。
// operations 不是恰好一条返回 Validation；Mutate 失败原样返回。
func (s Service) ApplyAgentChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet) (Projection, error) {
	if len(changeSet.Operations) != 1 {
		return Projection{}, apperr.Validation("立即写入只接受一条可逆改图命令；多步改图请提交提案")
	}
	return s.applyChangeSet(ctx, productID, graphID, changeSet, ActorAgent)
}

func (s Service) applyChangeSet(ctx context.Context, productID, graphID string, changeSet ChangeSet, actor ActorType) (Projection, error) {
	ctx = s.guardCtx(ctx)
	changeSet.ActorType = actor
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := Mutate(ctx, pgxTx, productID, graphID, changeSet, HistoryEdit)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// AgentProposalResult 是已落库、尚未应用到 live 图的 PENDING 提案身份。
type AgentProposalResult struct {
	ID                string
	GraphID           string
	BaseGraphRevision int // 提案相对的 live revision；确认时必须仍匹配
	Summary           string
}

// CreateAgentProposal 只存 PENDING 提案，不改 live 图。
// CreateProposal 的 Conflict / 库错误原样返回。
func (s Service) CreateAgentProposal(ctx context.Context, productID, conversationID string, changeSet ChangeSet) (AgentProposalResult, error) {
	var out AgentProposalResult
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := CreateProposal(ctx, pgxTx, productID, conversationID, changeSet)
		if err != nil {
			return err
		}
		out = result
		return nil
	})
	return out, err
}

// TryCurrent 给 Agent 工具与工作台在「图可能尚未出生」时读 active 图画布投影。
// 没有 active 图返回 nil, nil，不要改成 NotFound——HTTP current 才走 Current 的 404。
// 只读 Project，不写库。ctx 会挂 ProductGuard。不要和 Get（按图 id）搞混。
func (s Service) TryCurrent(ctx context.Context, productID string) (*Projection, error) {
	ctx = s.guardCtx(ctx)
	var out *Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := TryLoadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		proj, err := Project(ctx, pgxTx, *row)
		if err != nil {
			return err
		}
		out = &proj
		return nil
	})
	return out, err
}

// Undo 应用最近一条可逆编辑的 inverse。没有可撤销操作时返回 Conflict。
func (s Service) Undo(ctx context.Context, productID, graphID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := Undo(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// Redo 重做最近一次 Undo。栈顶不是 Undo 时返回 Conflict。
func (s Service) Redo(ctx context.Context, productID, graphID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		result, err := Redo(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// ConfirmProposal 把 PENDING 提案应用到 live 图。非 pending 返回 NotPending；revision 已变返回 Conflict。
func (s Service) ConfirmProposal(ctx context.Context, productID, graphID, proposalID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := ConfirmProposal(ctx, pgxTx, productID, graphID, proposalID)
		if err != nil {
			return err
		}
		active, err := loadActiveGraph(ctx, pgxTx, productID)
		if err != nil {
			active = row
		}
		if s.AfterProposalDecision != nil {
			if err := s.AfterProposalDecision(ctx, pgxTx, productID, graphID, proposalID, "confirmed"); err != nil {
				return err
			}
		}
		out, err = Project(ctx, pgxTx, active.Identity)
		return err
	})
	return out, err
}

// DiscardProposal 丢弃 PENDING 提案，不改 live 图。非 pending 返回 NotPending。
func (s Service) DiscardProposal(ctx context.Context, productID, graphID, proposalID string) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := DiscardProposal(ctx, pgxTx, productID, graphID, proposalID); err != nil {
			return err
		}
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		if s.AfterProposalDecision != nil {
			if err := s.AfterProposalDecision(ctx, pgxTx, productID, graphID, proposalID, "discarded"); err != nil {
				return err
			}
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// GetDocumentCandidate 读取内容节点上挂起的 AI 文稿候选。没有候选时返回 NotFound。
func (s Service) GetDocumentCandidate(ctx context.Context, productID, graphID, nodeID string) (DocumentCandidate, error) {
	ctx = s.guardCtx(ctx)
	var out DocumentCandidate
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		_, _, _, _, _, candidate, err := loadDocumentCandidate(ctx, pgxTx, productID, graphID, nodeID, false)
		out = candidate
		return err
	})
	return out, err
}

// ApplyDocumentCandidate 把候选 section 写入节点 config。revision 或 artifact 已变返回 Conflict；没有差异返回 Validation。
func (s Service) ApplyDocumentCandidate(ctx context.Context, productID, graphID, nodeID string, input ApplyDocumentCandidateInput) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, _, node, _, artifact, candidate, err := loadDocumentCandidate(ctx, pgxTx, productID, graphID, nodeID, true)
		if err != nil {
			return err
		}
		if input.BaseGraphRevision != row.Revision {
			return apperr.Conflict("图 revision 已变化，请刷新后重试")
		}
		if strings.TrimSpace(input.ArtifactID) == "" || input.ArtifactID != artifact.ID {
			return apperr.Conflict("文稿候选已变化，请刷新后重试")
		}
		if candidate.Status != "ready" {
			return apperr.Conflict("文稿或上游输入已变化，候选已过期")
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(artifact.PayloadJSON), &payload); err != nil {
			return err
		}
		proposed := proposedDocumentConfig(node, payload, candidate.DocumentAction)
		sectionKeys := normalizeSectionKeys(input.SectionKeys)
		changed := map[string]struct{}{}
		for _, section := range candidate.Sections {
			if section.Changed {
				changed[section.Key] = struct{}{}
			}
		}
		if len(changed) == 0 {
			return apperr.Validation("文稿候选与当前文稿没有差异")
		}
		for _, key := range sectionKeys {
			if _, ok := changed[key]; !ok {
				return apperr.Validation("只能应用包含差异的文稿 section")
			}
		}
		config, err := applyDocumentSections(node, proposed, sectionKeys)
		if err != nil {
			return err
		}
		origin := OriginCollaborative
		if len(sectionKeys) == 0 && (candidate.DocumentAction == DocumentActionRewrite || candidate.DocumentAction == DocumentActionReplace) {
			origin = OriginGenerated
		} else if DocumentOrigin(node) == OriginSeed {
			origin = OriginGenerated
		}
		result, err := Mutate(ctx, pgxTx, productID, graphID, ChangeSet{
			BaseGraphRevision: row.Revision,
			Summary:           "采用 AI 文稿建议",
			ActorType:         ActorUser,
			Operations: []Operation{UpdateNodeConfigOp{
				NodeRef: nodeID, Config: config, DocumentOrigin: strPtr(origin),
			}},
		}, HistoryEdit)
		if err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).
			Where("id = ? AND pending_candidate_artifact_id = ?", nodeID, artifact.ID).
			Update("pending_candidate_artifact_id", nil).Error; err != nil {
			return err
		}
		updated, err := loadGraph(ctx, pgxTx, productID, result.GraphID)
		if err != nil {
			return err
		}
		out, err = Project(ctx, pgxTx, updated.Identity)
		return err
	})
	return out, err
}

// DiscardDocumentCandidate 清掉 pending_candidate_artifact_id。artifact 已变返回 Conflict。
func (s Service) DiscardDocumentCandidate(ctx context.Context, productID, graphID, nodeID string, input DiscardDocumentCandidateInput) (Projection, error) {
	ctx = s.guardCtx(ctx)
	var out Projection
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, _, _, _, artifact, _, err := loadDocumentCandidate(ctx, pgxTx, productID, graphID, nodeID, true)
		if err != nil {
			return err
		}
		if strings.TrimSpace(input.ArtifactID) == "" || input.ArtifactID != artifact.ID {
			return apperr.Conflict("文稿候选已变化，请刷新后重试")
		}
		result := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).
			Where("id = ? AND pending_candidate_artifact_id = ?", nodeID, artifact.ID).
			Update("pending_candidate_artifact_id", nil)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperr.Conflict("文稿候选已变化，请刷新后重试")
		}
		out, err = Project(ctx, pgxTx, row.Identity)
		return err
	})
	return out, err
}

// ParseChangeSetReader 从 HTTP 请求体解析 ChangeSet；多余字段按 extra=forbid 拒绝。
// 读体失败原样返回；非法 JSON 或未知字段返回 Validation。
func ParseChangeSetReader(r io.Reader) (ChangeSet, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return ChangeSet{}, err
	}
	return ParseChangeSet(raw)
}

// SubmitRun 提交 GraphRun。一图同时只能有一个 running；其余 FIFO queued。同范围同目标请求合并。force 仅 node|to_node|selection 且须有显式目标。
// 图不存在返回 NotFound；非 active 或并发抢跑返回 Conflict；请求非法返回 Validation。
func (s Service) SubmitRun(ctx context.Context, productID, graphID string, req GraphRunRequest) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.SubmitRunTx(ctx, pgxTx, productID, graphID, req)
		return err
	})
	return out, err
}

// SubmitRunTx 在调用方已有的事务里提交 GraphRun。语义与 [Service.SubmitRun] 相同。
// 失败条件与 SubmitRun 相同。
func (s Service) SubmitRunTx(ctx context.Context, pgxTx *gorm.DB, productID, graphID string, req GraphRunRequest) (GraphRunResponse, error) {
	ctx = s.guardCtx(ctx)
	if req.Scope == "" {
		req.Scope = RunScopeGraph
	}
	submission, err := submitGraphRun(ctx, pgxTx, productID, graphID, req)
	if err != nil {
		return GraphRunResponse{}, err
	}
	return serializeGraphRun(submission.Run), nil
}

// PreviewRun 返回将入队节点的 planned_action，不写库、不入队。
// 请求非法返回 Validation；缺图返回 NotFound。
func (s Service) PreviewRun(ctx context.Context, productID, graphID string, req GraphRunRequest) (GraphRunPreviewResponse, error) {
	if req.Scope == "" {
		req.Scope = RunScopeGraph
	}
	if err := validateGraphRunRequest(req); err != nil {
		return GraphRunPreviewResponse{}, err
	}
	var out GraphRunPreviewResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		ctx := s.guardCtx(ctx)
		row, err := loadGraph(ctx, pgxTx, productID, graphID)
		if err != nil {
			return err
		}
		applied, err := loadAppliedGraph(ctx, pgxTx, row)
		if err != nil {
			return err
		}
		sources, _, _, _, err := loadGraphSources(ctx, pgxTx, row, applied)
		if err != nil {
			return err
		}
		nodes, err := PlanRun(applied, req.Scope, ptrStr(req.NodeID), req.NodeIDs, sources, req.Force, validDocumentAction(req.DocumentAction))
		if err != nil {
			return err
		}
		out = GraphRunPreviewResponse{
			Scope:            req.Scope,
			RequestedNodeID:  req.NodeID,
			RequestedNodeIDs: requestedNodeIDsOrEmpty(req.NodeIDs),
			Force:            req.Force,
			DocumentAction:   validDocumentAction(req.DocumentAction),
			Nodes:            nodes,
		}
		return nil
	})
	return out, err
}

// ListRuns 给 GET .../runs：按 started_at DESC 列出该图最近最多 20 条 GraphRun（含 node_runs）。
// 图不属于该商品返回 NotFound。不写库、不入队。完整事件流走 SSE，不要把本列表当游标分页。
func (s Service) ListRuns(ctx context.Context, productID, graphID string) (GraphRunListResponse, error) {
	var out GraphRunListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		runs, err := listGraphRuns(ctx, pgxTx, productID, graphID, 20)
		if err != nil {
			return err
		}
		items := make([]GraphRunResponse, 0, len(runs))
		for _, run := range runs {
			items = append(items, serializeGraphRun(run))
		}
		out = GraphRunListResponse{Items: items}
		return nil
	})
	return out, err
}

// GetRun 读取指定 GraphRun。找不到 run 返回 NotFound，不返回零值当成功。
func (s Service) GetRun(ctx context.Context, productID, graphID, runID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		run, err := loadGraphRun(ctx, pgxTx, productID, graphID, runID)
		if err != nil {
			return err
		}
		out = serializeGraphRun(run)
		return nil
	})
	return out, err
}

// GetRunForProduct 按 run id 读取运行，并校验属于该商品。workflowID 非空时还须匹配 graph_id。
// run 不存在、不属于该商品或 graph_id 不匹配返回 NotFound。
func (s Service) GetRunForProduct(ctx context.Context, productID, runID, workflowID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		run, err := loadGraphRunByID(ctx, pgxTx, runID)
		if err != nil {
			return err
		}
		if workflowID != "" && run.GraphID != workflowID {
			return apperr.NotFound("工作流运行不存在")
		}
		if _, err := loadGraph(ctx, pgxTx, productID, run.GraphID); err != nil {
			return err
		}
		out = serializeGraphRun(run)
		return nil
	})
	return out, err
}

// CancelRun 取消 queued 或 running 的 GraphRun。已取消则幂等返回当前行；已结束返回 Conflict。
func (s Service) CancelRun(ctx context.Context, productID, graphID, runID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.CancelRunTx(ctx, pgxTx, productID, graphID, runID)
		return err
	})
	return out, err
}

// CancelRunTx 在调用方已有的事务里取消 GraphRun。语义与 [Service.CancelRun] 相同。
// 缺图或缺 run 返回 NotFound；已结束（含 unknown）返回 Conflict。
func (s Service) CancelRunTx(ctx context.Context, pgxTx *gorm.DB, productID, graphID, runID string) (GraphRunResponse, error) {
	ctx = s.guardCtx(ctx)
	run, err := cancelGraphRun(ctx, pgxTx, productID, graphID, runID)
	if err != nil {
		return GraphRunResponse{}, err
	}
	if s.AfterRunStatus != nil {
		if err := s.AfterRunStatus(ctx, pgxTx, run.ID); err != nil {
			return GraphRunResponse{}, err
		}
	}
	return serializeGraphRun(run), nil
}

// RetryRun 仅对 failed 且 is_retryable 的运行再提交一次相同范围。unknown 不可经此重试。
// 非 failed 或不可重试返回 Validation；无法证明的 unknown 不得当失败自动重试。
func (s Service) RetryRun(ctx context.Context, productID, graphID, runID string) (GraphRunResponse, error) {
	var out GraphRunResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var err error
		out, err = s.RetryRunTx(ctx, pgxTx, productID, graphID, runID)
		return err
	})
	return out, err
}

// RetryRunTx 在调用方已有的事务里重试 failed 且 is_retryable 的 GraphRun。unknown 不可重试。
// 非 failed 或不可重试返回 Validation；无法证明的 unknown 不得当失败自动重试。
func (s Service) RetryRunTx(ctx context.Context, pgxTx *gorm.DB, productID, graphID, runID string) (GraphRunResponse, error) {
	ctx = s.guardCtx(ctx)
	submission, err := retryGraphRun(ctx, pgxTx, productID, graphID, runID)
	if err != nil {
		return GraphRunResponse{}, err
	}
	return serializeGraphRun(submission.Run), nil
}
