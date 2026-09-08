package graph

import (
	"bytes"
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// Undo 应用最近一条非 Undo 历史的 inverse。空 inverse 返回 Conflict。
func Undo(ctx context.Context, products ProductGuard, tx *gorm.DB, productID, graphID string) (CommandResult, error) {
	row, err := loadGraph(ctx, products, tx, productID, graphID)
	if err != nil {
		return CommandResult{}, err
	}
	last, err := lastOperationGroup(ctx, tx, row)
	if err != nil {
		return CommandResult{}, err
	}
	if last == nil || last.HistoryKind == HistoryUndo {
		return CommandResult{}, apperr.Conflict("没有可撤销的图操作")
	}
	inverse, err := inverseOperations(last.InverseOperationsJSON)
	if err != nil {
		return CommandResult{}, err
	}
	if len(inverse) == 0 {
		return CommandResult{}, apperr.Conflict("该操作没有可撤销的 inverse")
	}
	summary := clipSummary("撤销：" + sourceHistorySummary(last.Summary))
	return WriteTx(ctx, products, tx, Command{
		ProductID: productID,
		GraphID:   &graphID,
		ChangeSet: ChangeSet{
			BaseGraphRevision: row.Revision,
			Summary:           summary,
			ActorType:         ActorUser,
			Operations:        inverse,
		},
		Kind: HistoryUndo,
	})
}

// Redo 由 POST .../redo 与 Service.Redo 调用：只消费栈顶 HistoryUndo 的 inverse，再经 WriteTx 写成 HistoryRedo。
// 副作用：workflow_graphs.revision 递增，替换节点/边/分组，并追加 workflow_operation_groups。
// 栈顶不是 Undo 或 inverse 为空返回 Conflict（409）。空图画布从未编辑时 last 为 nil，同样 409。
// 不要把 Redo 实现成再调一次 Undo。Undo 之后若又写入 HistoryEdit，栈顶不再是 Undo，重做机会消失。
func Redo(ctx context.Context, products ProductGuard, tx *gorm.DB, productID, graphID string) (CommandResult, error) {
	row, err := loadGraph(ctx, products, tx, productID, graphID)
	if err != nil {
		return CommandResult{}, err
	}
	last, err := lastOperationGroup(ctx, tx, row)
	if err != nil {
		return CommandResult{}, err
	}
	if last == nil || last.HistoryKind != HistoryUndo {
		return CommandResult{}, apperr.Conflict("没有可重做的图操作")
	}
	inverse, err := inverseOperations(last.InverseOperationsJSON)
	if err != nil {
		return CommandResult{}, err
	}
	if len(inverse) == 0 {
		return CommandResult{}, apperr.Conflict("该操作没有可重做的 inverse")
	}
	summary := clipSummary("重做：" + sourceHistorySummary(last.Summary))
	return WriteTx(ctx, products, tx, Command{
		ProductID: productID,
		GraphID:   &graphID,
		ChangeSet: ChangeSet{
			BaseGraphRevision: row.Revision,
			Summary:           summary,
			ActorType:         ActorUser,
			Operations:        inverse,
		},
		Kind: HistoryRedo,
	})
}

func inverseOperations(raw []byte) ([]Operation, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[]")) || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	return unmarshalOperations(trimmed, true)
}

func sourceHistorySummary(summary string) string {
	for _, prefix := range []string{"撤销：", "重做："} {
		if strings.HasPrefix(summary, prefix) {
			return summary[len(prefix):]
		}
	}
	return summary
}

func clipSummary(summary string) string {
	runes := []rune(summary)
	if len(runes) > maxSummaryLen {
		return string(runes[:maxSummaryLen])
	}
	return summary
}
