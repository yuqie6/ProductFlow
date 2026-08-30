package graph

import (
	"bytes"
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

func Undo(ctx context.Context, tx *gorm.DB, productID, graphID string) (CommandResult, error) {
	row, err := loadGraph(ctx, tx, productID, graphID)
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
	return Mutate(ctx, tx, productID, graphID, ChangeSet{
		BaseGraphRevision: row.Revision,
		Summary:           summary,
		ActorType:         ActorUser,
		Operations:        inverse,
	}, HistoryUndo)
}

func Redo(ctx context.Context, tx *gorm.DB, productID, graphID string) (CommandResult, error) {
	row, err := loadGraph(ctx, tx, productID, graphID)
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
	return Mutate(ctx, tx, productID, graphID, ChangeSet{
		BaseGraphRevision: row.Revision,
		Summary:           summary,
		ActorType:         ActorUser,
		Operations:        inverse,
	}, HistoryRedo)
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
