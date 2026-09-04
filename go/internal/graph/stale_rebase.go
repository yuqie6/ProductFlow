package graph

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// rebaseStaleNodeConfigIfSafe 让检查器/Agent 一次 update_node_config 在并行 cook 顶 revision 后仍能落地。
// 仅当 ops 全是 UpdateNodeConfigOp，且 base 之后的历史也全是其它节点的 update_node_config 时，把 BaseGraphRevision 提到当前。
// 同节点丢失更新、拓扑/改名/移动、历史缺口或未来 revision 不 rebase，交给 Apply 返回 409。
func rebaseStaleNodeConfigIfSafe(ctx context.Context, tx *gorm.DB, row graphRow, changeSet *ChangeSet) error {
	if changeSet.BaseGraphRevision == row.Revision {
		return nil
	}
	if changeSet.BaseGraphRevision < 0 || changeSet.BaseGraphRevision > row.Revision {
		return nil
	}
	targets, ok := nodeConfigWriteTargets(changeSet.Operations)
	if !ok {
		return nil
	}
	scan, err := scanNodeConfigHistoryAfter(ctx, tx, row.ID, changeSet.BaseGraphRevision, row.Revision)
	if err != nil {
		return err
	}
	if scan.unsafe {
		return nil
	}
	for _, id := range targets {
		if _, hit := scan.touched[id]; hit {
			return nil
		}
	}
	changeSet.BaseGraphRevision = row.Revision
	return nil
}

func nodeConfigWriteTargets(ops []Operation) ([]string, bool) {
	if len(ops) == 0 {
		return nil, false
	}
	seen := map[string]struct{}{}
	targets := make([]string, 0, len(ops))
	for _, op := range ops {
		update, ok := op.(UpdateNodeConfigOp)
		if !ok {
			return nil, false
		}
		id := strings.TrimSpace(update.NodeRef)
		if id == "" {
			return nil, false
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		targets = append(targets, id)
	}
	return targets, true
}

type nodeConfigHistoryScan struct {
	unsafe  bool
	touched map[string]struct{}
}

func scanNodeConfigHistoryAfter(ctx context.Context, tx *gorm.DB, graphID string, baseRevision, currentRevision int) (nodeConfigHistoryScan, error) {
	failClosed := nodeConfigHistoryScan{unsafe: true}
	if currentRevision <= baseRevision {
		return failClosed, nil
	}
	var groups []schema.WorkflowOperationGroups
	err := tx.WithContext(ctx).
		Where("graph_id = ? AND result_revision > ? AND result_revision <= ?", graphID, baseRevision, currentRevision).
		Order("result_revision").
		Find(&groups).Error
	if err != nil {
		return nodeConfigHistoryScan{}, err
	}
	if len(groups) != currentRevision-baseRevision {
		return failClosed, nil
	}
	touched := map[string]struct{}{}
	for _, group := range groups {
		ops, err := unmarshalOperations([]byte(group.OperationsJSON), true)
		if err != nil {
			return failClosed, nil
		}
		for _, op := range ops {
			update, ok := op.(UpdateNodeConfigOp)
			if !ok {
				return failClosed, nil
			}
			id := strings.TrimSpace(update.NodeRef)
			if id == "" {
				return failClosed, nil
			}
			touched[id] = struct{}{}
		}
	}
	return nodeConfigHistoryScan{touched: touched}, nil
}
