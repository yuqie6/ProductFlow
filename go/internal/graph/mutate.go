package graph

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"gorm.io/gorm"
)

// CreateEmpty 持久化一张空的 active schema-v3 图。空画布不能作为 no-op ChangeSet 出生。
func CreateEmpty(ctx context.Context, tx *gorm.DB, productID, title string) (GraphRow, error) {
	if err := lockProduct(ctx, tx, productID); err != nil {
		return GraphRow{}, err
	}
	exists, err := activeGraphExists(ctx, tx, productID)
	if err != nil {
		return GraphRow{}, err
	}
	if exists {
		return GraphRow{}, apperr.Conflict("商品已有 active schema-v3 工作流")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = DefaultGraphTitle
	}
	graphID := clockid.New()
	_, err = pfdb.Exec(ctx, tx, `
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES ($1, $2, $3, TRUE, $4, 1, NOW(), NOW())
	`, graphID, productID, title, SchemaVersion)
	if err != nil {
		return GraphRow{}, err
	}
	return GraphRow{
		ID:            graphID,
		ProductID:     productID,
		Title:         title,
		Active:        true,
		SchemaVersion: SchemaVersion,
		Revision:      1,
	}, nil
}

// Mutate 把 ChangeSet 应用到已有 active schema-v3 图；只 flush 不 commit。
func Mutate(ctx context.Context, tx *gorm.DB, productID, graphID string, changeSet ChangeSet, kind HistoryKind) (CommandResult, error) {
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
	_, err = pfdb.Exec(ctx, tx, `
		UPDATE workflow_graphs SET revision = $2, updated_at = NOW() WHERE id = $1
	`, row.ID, after.Revision)
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
