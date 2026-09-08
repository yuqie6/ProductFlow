package graph

import (
	"context"
	"errors"
	"net/http"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// Command 是调用方已有 GORM 事务时提交的 live 图写入意图。
// 图包负责 active/schema-v3、行锁、expected graph identity、revision、历史记录和 flush；
// 只 flush 不 commit，调用方事务提交或回滚。
type Command struct {
	ProductID string
	Title     string // 仅新建图使用；空则 DefaultGraphTitle
	// GraphID 为 nil 时新建图；非 nil 时改该图。
	GraphID *string
	// RequireActive 在改图前锁商品当前 active 图：缺失或 id 与 GraphID 不同则 Conflict。
	// 新建图忽略此位。
	RequireActive bool
	ChangeSet     ChangeSet
	Kind          HistoryKind // 空则 HistoryEdit
}

// Live 是商品当前 active 图的身份与已应用快照，供预览/计数等纯计算读取。
type Live struct {
	Identity Identity
	Applied  AppliedGraph
}

// WriteTx 在调用方事务里执行 Graph Command。GraphID 为空则新建，否则改已有图。
// 不自行 commit。base_graph_revision 不匹配、已有 active 图、RequireActive 对不上时返回 Conflict。
func WriteTx(ctx context.Context, products ProductGuard, tx *gorm.DB, cmd Command) (CommandResult, error) {
	kind := cmd.Kind
	if kind == "" {
		kind = HistoryEdit
	}
	if cmd.GraphID == nil {
		return stageNew(ctx, products, tx, cmd.ProductID, cmd.Title, cmd.ChangeSet)
	}
	graphID := *cmd.GraphID
	if cmd.RequireActive {
		live, err := loadActiveGraphForUpdate(ctx, products, tx, cmd.ProductID)
		if err != nil {
			return CommandResult{}, err
		}
		if live == nil {
			return CommandResult{}, apperr.Conflict("商品没有可写入的 schema-v3 工作流")
		}
		if live.ID != graphID {
			return CommandResult{}, apperr.Conflict("工作流已变化，请重新预览后重试")
		}
	}
	return mutate(ctx, products, tx, cmd.ProductID, graphID, cmd.ChangeSet, kind)
}

// ProjectCommand 把刚写入的 CommandResult 展开成画布 HTTP 投影。
func ProjectCommand(ctx context.Context, products ProductGuard, tx *gorm.DB, result CommandResult) (Projection, error) {
	return Project(ctx, products, tx, result.identity())
}

// ProjectGraph 按商品与图 id 读取画布投影。对不上返回 NotFound。
func ProjectGraph(ctx context.Context, products ProductGuard, tx *gorm.DB, productID, graphID string) (Projection, error) {
	row, err := loadGraph(ctx, products, tx, productID, graphID)
	if err != nil {
		return Projection{}, err
	}
	return Project(ctx, products, tx, row.Identity)
}

func (r CommandResult) identity() Identity {
	return Identity{
		ID:            r.GraphID,
		ProductID:     r.ProductID,
		Title:         r.Title,
		Active:        r.Active,
		SchemaVersion: r.SchemaVersion,
		Revision:      r.Revision,
	}
}

// TryLive 读取商品当前 active 图。没有 active 图返回 nil, nil，不报 NotFound。
func TryLive(ctx context.Context, products ProductGuard, tx *gorm.DB, productID string) (*Live, error) {
	row, err := loadActiveGraph(ctx, products, tx, productID)
	if err != nil {
		var e apperr.Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return liveFromRow(ctx, tx, row)
}

// TryLiveForUpdate 锁住商品当前 active 图并展开快照。没有 active 图返回 nil, nil。
func TryLiveForUpdate(ctx context.Context, products ProductGuard, tx *gorm.DB, productID string) (*Live, error) {
	row, err := loadActiveGraphForUpdate(ctx, products, tx, productID)
	if err != nil || row == nil {
		return nil, err
	}
	return liveFromRow(ctx, tx, *row)
}

// LoadLiveForUpdate 锁指定 live 图并校验 active schema-v3 与 expected revision。
// 非 active 或非 schema-v3 返回 Conflict；revision 对不上返回 Conflict；缺图返回 NotFound。
func LoadLiveForUpdate(ctx context.Context, products ProductGuard, tx *gorm.DB, productID, graphID string, expectedRevision int) (Live, error) {
	row, err := loadGraphForUpdate(ctx, products, tx, productID, graphID)
	if err != nil {
		return Live{}, err
	}
	if !row.Active || row.SchemaVersion != SchemaVersion {
		return Live{}, apperr.Conflict("只能从 active schema-v3 工作流保存配方")
	}
	if row.Revision != expectedRevision {
		return Live{}, apperr.Conflict("工作流已变化，请刷新后重试")
	}
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return Live{}, err
	}
	return Live{Identity: row.Identity, Applied: applied}, nil
}

func liveFromRow(ctx context.Context, tx *gorm.DB, row graphRow) (*Live, error) {
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return nil, err
	}
	return &Live{Identity: row.Identity, Applied: applied}, nil
}

// ExpandBirth 把名称-only 图（恰好一个 product_source）按模板展开套图。
// 没有 live 图则新建完整模板；已有其它节点则不改图并返回 false。
// 未选图种或缺参考图返回 false, nil，不报 Validation。只 flush 不 commit。
func ExpandBirth(ctx context.Context, products ProductGuard, tx *gorm.DB, productID, title string, in DirectCreateInput) (bool, CommandResult, error) {
	if len(in.ImageTypes) == 0 || len(in.ReferenceAssetIDs) == 0 {
		return false, CommandResult{}, nil
	}
	live, err := TryLiveForUpdate(ctx, products, tx, productID)
	if err != nil {
		return false, CommandResult{}, err
	}
	if live == nil {
		changeSet, err := BuildDirectCreateTemplate(in)
		if err != nil {
			return false, CommandResult{}, err
		}
		result, err := WriteTx(ctx, products, tx, Command{
			ProductID: productID,
			Title:     title,
			ChangeSet: changeSet,
		})
		if err != nil {
			return false, CommandResult{}, err
		}
		return true, result, nil
	}
	var productSources []AppliedNode
	for _, node := range live.Applied.Nodes {
		if node.NodeType == NodeProductSource {
			productSources = append(productSources, node)
			continue
		}
		return false, CommandResult{}, nil
	}
	if len(productSources) != 1 {
		return false, CommandResult{}, nil
	}
	changeSet, err := TemplateForExistingProductSource(productSources[0].ID, live.Applied.Revision, in)
	if err != nil {
		return false, CommandResult{}, err
	}
	graphID := live.Identity.ID
	result, err := WriteTx(ctx, products, tx, Command{
		ProductID: productID,
		GraphID:   &graphID,
		ChangeSet: changeSet,
	})
	if err != nil {
		return false, CommandResult{}, err
	}
	return true, result, nil
}
