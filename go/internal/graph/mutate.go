package graph

import (
	"context"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// CreateEmpty 持久化一张空的 active schema-v3 图。空画布不能作为 no-op ChangeSet 出生。
// 商品已有 active 图时返回 Conflict。缺守卫返回 Internal，商品不存在返回 NotFound。
func CreateEmpty(ctx context.Context, tx *gorm.DB, productID, title string) (graphRow, error) {
	if err := lockProduct(ctx, tx, productID); err != nil {
		return graphRow{}, err
	}
	exists, err := activeGraphExists(ctx, tx, productID)
	if err != nil {
		return graphRow{}, err
	}
	if exists {
		return graphRow{}, apperr.Conflict("商品已有 active schema-v3 工作流")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = DefaultGraphTitle
	}
	graphID := clockid.New()
	now := time.Now().UTC()
	err = tx.WithContext(ctx).Create(&schema.WorkflowGraphs{
		ID:            graphID,
		ProductID:     productID,
		Title:         title,
		Active:        true,
		SchemaVersion: SchemaVersion,
		Revision:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}).Error
	if err != nil {
		return graphRow{}, err
	}
	return graphRow{Identity: Identity{
		ID:            graphID,
		ProductID:     productID,
		Title:         title,
		Active:        true,
		SchemaVersion: SchemaVersion,
		Revision:      1,
	}}, nil
}

// mutate 把 ChangeSet 应用到已有 active schema-v3 图；只 flush 不 commit。
// 跨包写入走 WriteTx。非 active 或非 schema-v3 返回 Conflict；缺图返回 NotFound。
func mutate(ctx context.Context, tx *gorm.DB, productID, graphID string, changeSet ChangeSet, kind HistoryKind) (CommandResult, error) {
	if err := lockRunningGraphRunsForUpdate(ctx, tx, graphID); err != nil {
		return CommandResult{}, err
	}
	row, err := loadGraphForUpdate(ctx, tx, productID, graphID)
	if err != nil {
		return CommandResult{}, err
	}
	if !row.Active {
		return CommandResult{}, apperr.Conflict("只能修改 active schema-v3 工作流")
	}
	if row.SchemaVersion != SchemaVersion {
		return CommandResult{}, apperr.Conflict("画布修改只支持 schema-v3 工作流")
	}
	if err := rebaseStaleNodeConfigIfSafe(ctx, tx, row, &changeSet); err != nil {
		return CommandResult{}, err
	}
	before, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return CommandResult{}, err
	}
	proposed, err := Apply(before, changeSet)
	if err != nil {
		return CommandResult{}, err
	}
	after := assignPersistentIDs(before, proposed, clockid.New)
	if err := validateBoundAssets(ctx, tx, productID, after); err != nil {
		return CommandResult{}, err
	}
	if err := validateProductSourceConfigs(ctx, tx, productID, after); err != nil {
		return CommandResult{}, err
	}
	err = tx.WithContext(ctx).Model(&schema.WorkflowGraphs{}).Where("id = ?", row.ID).Updates(map[string]any{
		"revision":   after.Revision,
		"updated_at": time.Now().UTC(),
	}).Error
	if err != nil {
		return CommandResult{}, err
	}
	if err := replaceGraphContents(ctx, tx, row.ID, after); err != nil {
		return CommandResult{}, err
	}
	actor := changeSet.ActorType
	if actor == "" {
		actor = ActorUser
	}
	operationGroupID, err := recordOperationGroup(ctx, tx, row.ID, ChangeSet{
		Summary:    changeSet.Summary,
		ActorType:  actor,
		Operations: changeSet.Operations,
	}, Invert(before, after), before.Revision, after.Revision, kind)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{
		GraphID:          row.ID,
		ProductID:        row.ProductID,
		Title:            row.Title,
		Active:           row.Active,
		SchemaVersion:    row.SchemaVersion,
		Revision:         after.Revision,
		Applied:          after,
		OperationGroupID: operationGroupID,
		HistoryKind:      kind,
	}, nil
}
