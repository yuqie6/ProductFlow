package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const (
	effectResultApplied = "applied"
	effectResultFailed  = "failed"
	effectResultUnknown = "unknown"

	reconStateApplied  = "applied"
	reconStateConflict = "conflict"
	reconStateUnknown  = "unknown"
)

type effectReconcileOutcome struct {
	ID                  string
	ToolName            string
	ToolCallID          string
	IdempotencyKey      string
	EffectResult        string
	ReconciliationState string
	Result              json.RawMessage
	Detail              *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func recoverableToolNames() []string {
	names := make([]string, 0, len(toolRecoveryPolicies))
	for name, policy := range toolRecoveryPolicies {
		if policy == "reconcile_then_retry" || policy == "reconcile_only" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// reconcileEffectIntent 按 tool_call_id 对账副作用；已 applied/failed 的对账行直接返回，unknown 才继续。
func (s Service) reconcileEffectIntent(
	ctx context.Context,
	gdb *gorm.DB,
	conversationID, projectionID string,
	intent toolEffectIntentV1,
	allowRetry bool,
) (effectReconcileOutcome, error) {
	policy := toolRecoveryPolicies[intent.ToolName]
	if policy == "" || policy == "none" {
		return effectReconcileOutcome{}, apperr.Conflict("该工具不支持副作用对账")
	}
	if intent.RecoveryPolicy != "" && intent.RecoveryPolicy != policy {
		return effectReconcileOutcome{}, apperr.Conflict("副作用 intent 的 recovery_policy 与清单不一致")
	}
	var existing schema.AgentTurnEffectReconciliations
	err := gdb.Clauses(pfdb.ForUpdate()).
		Where("turn_projection_id = ? AND tool_call_id = ?", projectionID, intent.ToolCallID).
		Take(&existing).Error
	if err == nil && existing.EffectResult != effectResultUnknown {
		return outcomeFromReconciliation(existing), nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return effectReconcileOutcome{}, err
	}

	reconciled, reconErr := s.lookupEffectState(ctx, conversationID, intent)
	if reconErr != nil {
		return s.persistEffectReconciliation(gdb, projectionID, existing, intent, ReconcileResponse{
			State:  reconStateUnknown,
			Detail: ptr("副作用对账过程中出错，结果仍不明确"),
		})
	}
	if reconciled.State == "not_applied" && allowRetry && policy == "reconcile_then_retry" {
		retryResult, retryErr := s.retryEffectIntent(ctx, conversationID, intent)
		if retryErr == nil {
			raw, _ := json.Marshal(retryResult)
			detail := "工具副作用已按原幂等键重试提交"
			return s.persistEffectReconciliation(gdb, projectionID, existing, intent, ReconcileResponse{
				State:  reconStateApplied,
				Result: raw,
				Detail: &detail,
			})
		}
		second, secondErr := s.lookupEffectState(ctx, conversationID, intent)
		if secondErr != nil {
			return s.persistEffectReconciliation(gdb, projectionID, existing, intent, ReconcileResponse{
				State:  reconStateUnknown,
				Detail: ptr("重试后副作用对账过程中出错，结果仍不明确"),
			})
		}
		reconciled = second
	}
	return s.persistEffectReconciliation(gdb, projectionID, existing, intent, reconciled)
}

// lookupEffectState 按工具名查对应账本或 reconcile API，不重试副作用。payload 无法解析或缺少关键字段时保持 unknown，不猜 applied。
//
// reconcileEffectIntent 在落对账行前调用。图工具走 ReconcileGraphTool；跑图请求走 ReconcileWorkflowRunRequest。
func (s Service) lookupEffectState(ctx context.Context, conversationID string, intent toolEffectIntentV1) (ReconcileResponse, error) {
	fields, err := decodeIntentPayload(intent.RequestPayload)
	if err != nil {
		return ReconcileResponse{State: reconStateUnknown, Detail: ptr("副作用 intent payload 无法解析")}, nil
	}
	switch intent.ToolName {
	case "finalize_product_intake_v1":
		selection, ok := fields["selection"]
		if !ok {
			return ReconcileResponse{State: reconStateUnknown, Detail: ptr("product intake 缺少 selection")}, nil
		}
		ids := fieldStrings(fields, "reference_asset_ids")
		return s.ReconcileTool(ctx, conversationID, "finalize_product_intake_v1", intent.IdempotencyKey, toolPrepared(conversationID, "finalize_product_intake_v1", map[string]any{}, map[string]any{
			"selection": selection, "reference_asset_ids": ids,
		}))
	case "create_product_workspace_v1":
		return s.ReconcileWorkspaceFromGlobal(ctx, conversationID, fieldString(fields, "name"), intent.IdempotencyKey)
	case applyGraphTool, proposeGraphTool:
		changeSet, ok := fields["change_set"]
		if !ok {
			return ReconcileResponse{State: reconStateUnknown, Detail: ptr("graph 工具缺少 change_set")}, nil
		}
		before, target, _, parseErr := graphChangeSetPrepared(changeSet)
		if parseErr != nil {
			return ReconcileResponse{State: reconStateUnknown, Detail: ptr("graph change_set 无法对账")}, nil
		}
		return s.ReconcileGraphTool(ctx, conversationID, intent.ToolName, intent.IdempotencyKey, before, target)
	case discardProposalTool:
		proposalID := fieldOptionalString(fields, "proposal_id")
		target := map[string]any{"proposal_id": any(nil)}
		if proposalID != nil {
			target["proposal_id"] = *proposalID
		}
		return s.ReconcileGraphTool(ctx, conversationID, discardProposalTool, intent.IdempotencyKey, map[string]any{}, target)
	case cancelRunTool:
		return s.ReconcileGraphTool(ctx, conversationID, cancelRunTool, intent.IdempotencyKey, map[string]any{}, map[string]any{
			"run_id": fieldString(fields, "run_id"),
		})
	case "request_workflow_run_v1", "request_global_workflow_run_v1":
		spec, specErr := runScopeFromFields(fields)
		if specErr != nil {
			return ReconcileResponse{State: reconStateUnknown, Detail: ptr("workflow request scope 无法对账")}, nil
		}
		if intent.ToolName == "request_global_workflow_run_v1" {
			return s.ReconcileGlobalWorkflowRunRequest(
				ctx, conversationID, intent.IdempotencyKey,
				fieldString(fields, "product_id"), fieldString(fields, "workflow_id"), fieldString(fields, "source_step_id"),
				fieldInt(fields, "expected_workflow_revision"), fieldOptionalString(fields, "task_id"), fieldOptionalString(fields, "source_run_id"), spec,
			)
		}
		return s.ReconcileWorkflowRunRequest(
			ctx, conversationID, intent.IdempotencyKey,
			fieldString(fields, "product_id"), fieldString(fields, "workflow_id"), fieldString(fields, "source_step_id"),
			fieldInt(fields, "expected_workflow_revision"), fieldOptionalString(fields, "task_id"), fieldOptionalString(fields, "source_run_id"), spec,
		)
	default:
		return ReconcileResponse{State: reconStateUnknown, Detail: ptr("该工具没有对账实现")}, nil
	}
}

// retryEffectIntent 仅在 policy=reconcile_then_retry 且 lookup 为 not_applied 时，按原幂等键重放工具入口。
//
// 由 reconcileEffectIntent 调用。不支持的工具返回 Conflict。重试后仍对不上必须再 lookup，不可证明则 unknown。
func (s Service) retryEffectIntent(ctx context.Context, conversationID string, intent toolEffectIntentV1) (any, error) {
	fields, err := decodeIntentPayload(intent.RequestPayload)
	if err != nil {
		return nil, err
	}
	key := intent.IdempotencyKey
	switch intent.ToolName {
	case "finalize_product_intake_v1":
		selection, ok := fields["selection"]
		if !ok {
			return nil, apperr.Validation("product intake 缺少 selection")
		}
		return s.FinalizeProductIntake(ctx, conversationID, key, selection, fieldStrings(fields, "reference_asset_ids"))
	case "create_product_workspace_v1":
		return s.LaunchWorkspaceFromGlobal(ctx, conversationID, fieldString(fields, "name"), key)
	case applyGraphTool:
		changeSet, ok := fields["change_set"]
		if !ok {
			return nil, apperr.Validation("graph 工具缺少 change_set")
		}
		return s.ApplyGraphTool(ctx, conversationID, changeSet, key)
	case proposeGraphTool:
		changeSet, ok := fields["change_set"]
		if !ok {
			return nil, apperr.Validation("graph 工具缺少 change_set")
		}
		return s.ProposeGraphTool(ctx, conversationID, changeSet, key)
	case discardProposalTool:
		id := ""
		if value := fieldOptionalString(fields, "proposal_id"); value != nil {
			id = *value
		}
		return s.DiscardProposalTool(ctx, conversationID, id, key)
	case cancelRunTool:
		return s.CancelRunTool(ctx, conversationID, fieldString(fields, "run_id"), key)
	case "request_workflow_run_v1", "request_global_workflow_run_v1":
		spec, specErr := runScopeFromFields(fields)
		if specErr != nil {
			return nil, specErr
		}
		if intent.ToolName == "request_global_workflow_run_v1" {
			return s.CreateGlobalWorkflowRunRequest(
				ctx, conversationID, fieldString(fields, "product_id"), fieldString(fields, "workflow_id"),
				key, fieldString(fields, "source_step_id"), fieldInt(fields, "expected_workflow_revision"),
				fieldOptionalString(fields, "task_id"), fieldOptionalString(fields, "source_run_id"), spec,
			)
		}
		return s.CreateWorkflowRunRequest(
			ctx, conversationID, fieldString(fields, "workflow_id"),
			key, fieldString(fields, "source_step_id"), fieldInt(fields, "expected_workflow_revision"),
			fieldOptionalString(fields, "task_id"), fieldOptionalString(fields, "source_run_id"), spec,
		)
	default:
		return nil, apperr.Conflict("该工具不支持副作用重试")
	}
}

// persistEffectReconciliation 按 turn_projection_id + tool_call_id 写入或更新 agent_turn_effect_reconciliations。
//
// 唯一约束冲突回放已有行。not_applied 不能当终态，finalizeEffectStates 会收成 unknown。已 applied/failed 的行不应被本函数改回 unknown（调用方先短路）。
func (s Service) persistEffectReconciliation(
	gdb *gorm.DB,
	projectionID string,
	existing schema.AgentTurnEffectReconciliations,
	intent toolEffectIntentV1,
	reconciled ReconcileResponse,
) (effectReconcileOutcome, error) {
	effect, state := finalizeEffectStates(reconciled)
	var resultJSON *string
	if len(reconciled.Result) > 0 {
		s := string(reconciled.Result)
		resultJSON = &s
	}
	now := time.Now().UTC()
	id := existing.ID
	if id == "" {
		id = newID()
		rec := schema.AgentTurnEffectReconciliations{
			ID: id, TurnProjectionID: projectionID, ToolCallID: intent.ToolCallID,
			ToolName: intent.ToolName, IdempotencyKey: intent.IdempotencyKey,
			EffectResult: effect, ReconciliationState: state, ResultJSON: resultJSON,
			Detail: reconciled.Detail, CreatedAt: now, UpdatedAt: now,
		}
		if err := gdb.Create(&rec).Error; err != nil {
			if uniqueViolation(err) {
				var saved schema.AgentTurnEffectReconciliations
				if takeErr := gdb.Where("turn_projection_id = ? AND tool_call_id = ?", projectionID, intent.ToolCallID).Take(&saved).Error; takeErr != nil {
					return effectReconcileOutcome{}, takeErr
				}
				return outcomeFromReconciliation(saved), nil
			}
			return effectReconcileOutcome{}, err
		}
	} else if err := gdb.Model(&schema.AgentTurnEffectReconciliations{}).Where("id = ?", id).Updates(map[string]any{
		"effect_result":        effect,
		"reconciliation_state": state,
		"result_json":          resultJSON,
		"detail":               reconciled.Detail,
		"updated_at":           now,
	}).Error; err != nil {
		return effectReconcileOutcome{}, err
	}
	var saved schema.AgentTurnEffectReconciliations
	if err := gdb.Where("id = ?", id).Take(&saved).Error; err != nil {
		return effectReconcileOutcome{}, err
	}
	return outcomeFromReconciliation(saved), nil
}

func finalizeEffectStates(reconciled ReconcileResponse) (effectResult, reconState string) {
	switch reconciled.State {
	case reconStateApplied:
		if len(reconciled.Result) == 0 {
			return effectResultUnknown, reconStateUnknown
		}
		return effectResultApplied, reconStateApplied
	case reconStateConflict:
		return effectResultFailed, reconStateConflict
	default:
		// not_applied 与无法分类的状态都不能作为最终落库态。
		return effectResultUnknown, reconStateUnknown
	}
}

func outcomeFromReconciliation(row schema.AgentTurnEffectReconciliations) effectReconcileOutcome {
	out := effectReconcileOutcome{
		ID: row.ID, ToolName: row.ToolName, ToolCallID: row.ToolCallID, IdempotencyKey: row.IdempotencyKey,
		EffectResult: row.EffectResult, ReconciliationState: row.ReconciliationState,
		Detail: row.Detail, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.ResultJSON != nil {
		out.Result = json.RawMessage(*row.ResultJSON)
	}
	return out
}

func decodeIntentPayload(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func fieldString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func fieldOptionalString(fields map[string]json.RawMessage, key string) *string {
	raw, ok := fields[key]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	value := fieldString(fields, key)
	if value == "" {
		return nil
	}
	return &value
}

func fieldInt(fields map[string]json.RawMessage, key string) int {
	raw, ok := fields[key]
	if !ok {
		return 0
	}
	var number float64
	if json.Unmarshal(raw, &number) != nil {
		return 0
	}
	return int(number)
}

func fieldBool(fields map[string]json.RawMessage, key string) bool {
	raw, ok := fields[key]
	if !ok {
		return false
	}
	var value bool
	_ = json.Unmarshal(raw, &value)
	return value
}

func fieldStrings(fields map[string]json.RawMessage, key string) []string {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func runScopeFromFields(fields map[string]json.RawMessage) (runScopeSpec, error) {
	return parseRunScopeSpec(
		fieldString(fields, "scope"),
		fieldOptionalString(fields, "node_id"),
		fieldStrings(fields, "node_ids"),
		fieldBool(fields, "force"),
		fieldString(fields, "document_action"),
	)
}
