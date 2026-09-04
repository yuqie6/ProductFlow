// Package recipe 仅从 live schema-v3 显式保存配方；应用走 Graph Command。
//
// 职责：把当前图的结构/配置存成可复用配方。完整配方不能 merge 进已有 live 图（只能应用到空商品）；
// fragment（组或选区）可以 merge，或返回显式冲突。配方不存商品身份、绑定资产、生成结果或媒体 bytes。
// 调用时机：用户从画布显式保存；预览后再 Apply。不要在 Agent 工具里偷偷建配方。
// 副作用：写 workflow_recipes / versions / applications；Apply 经 graph 命令改目标商品的 live 图。
// 错误：目标已有图且配方是完整图 → Conflict；选区对不上 → Validation。
package recipe

import (
	"context"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Service 拥有配方提取、预览与应用；应用走 Graph Command。
type Service struct {
	DB       *gorm.DB           // 命令事务
	Products graph.ProductGuard // 锁目标商品行，避免 graph import product
}

// CreateInput 从 live schema-v3 显式保存一条新配方。
type CreateInput struct {
	ProductID                      string
	WorkflowID                     string
	SourceType                     string // workflow | group | selection
	GroupID                        *string
	NodeIDs                        []string // 仅 selection
	ExpectedGraphRevision          int      // 对不上则 409
	Title                          string
	Description                    *string // nil 表示未写说明
	PreferredVisualSystemVersionID *string
}

// AppendInput 在已有配方上追加一版，仍从 live 图显式提取。
type AppendInput struct {
	CreateInput
	RecipeID              string
	ExpectedRecipeVersion int // 对不上则 409
}

// ApplyInput 把配方应用到目标商品 live 图；IdempotencyKey 绑定同一次确认。
type ApplyInput struct {
	ProductID             string
	RecipeID              string
	ExpectedRecipeVersion int    // 对不上则 409
	ExpectedGraphRevision int    // 目标图变了则 409
	PreviewDigest         string // 必须与 Preview 一致
	IdempotencyKey        string // 绑定同一次确认；命中则回放
}

// ApplicationResult 是一次配方 Apply 的内部结果，HTTP 用 ApplicationView。
// Created=false 表示相同幂等键已应用，回放。Mode=create 空画布；merge 只允许 fragment。
// Graph 来自 Graph Command，本包不直接写 workflow_graphs。
type ApplicationResult struct {
	Created           bool // false 表示相同幂等键已应用过，本次回放
	RecipeID          string
	RecipeVersionID   string
	RecipeVersion     int              // 实际应用的配方版本
	Mode              string           // create | merge；merge 只允许 fragment
	Graph             graph.Projection // 来自 Graph Command，本包不直接写 workflow_graphs
	AddedNodeIDs      []string         // 本次写入的 live 节点
	AddedEdgeIDs      []string         // 本次写入的 live 边
	UpdatedNodeIDs    []string         // create 模式应为 []
	BaseGraphRevision *int             // 预览时的图 revision
	PreviewDigest     *string          // 与 Preview 相同的 digest
	RequiredBindings  []string         // 如 product_identity
}

// List 列出配方摘要（当前版本，不含历史 Versions）。
// 调用时机：HTTP GET /workflow-recipes。includeArchived=false 时跳过已归档。空列表返回 [] 不是 nil。
func (s Service) List(ctx context.Context, includeArchived bool) ([]RecipeView, error) {
	var out []RecipeView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		rows, err := listRecipes(ctx, pgxTx, includeArchived)
		if err != nil {
			return err
		}
		out = make([]RecipeView, 0, len(rows))
		for _, rec := range rows {
			view, err := serializeSummary(rec)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return nil
	})
	if out == nil {
		out = []RecipeView{}
	}
	return out, err
}

// Get 读取配方及其全部版本。调用时机：HTTP GET /:recipe_id。找不到 NotFound。
// current version 缺失返回 Conflict，不要把半残行给前端。
func (s Service) Get(ctx context.Context, recipeID string) (RecipeView, error) {
	var out RecipeView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		rec, err := loadRecipe(ctx, pgxTx, recipeID, false)
		if err != nil {
			return err
		}
		out, err = serializeRecipe(rec)
		return err
	})
	return out, err
}

// Create 从当前 live schema-v3 显式提取并保存配方。
// 图不存在返回 NotFound；非 active 或 revision 已变返回 Conflict；选区非法或标题为空返回 Validation。
func (s Service) Create(ctx context.Context, in CreateInput) (RecipeView, error) {
	var out RecipeView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		payload, err := extractLive(ctx, pgxTx, in)
		if err != nil {
			return err
		}
		preferred, err := optionalVisual(ctx, pgxTx, in.PreferredVisualSystemVersionID)
		if err != nil {
			return err
		}
		title, description, err := normalizeTitle(in.Title, in.Description)
		if err != nil {
			return err
		}
		encoded, err := payloadJSON(payload)
		if err != nil {
			return err
		}
		hash, err := payloadHash(payload)
		if err != nil {
			return err
		}
		recipeID := clockid.New()
		versionID := clockid.New()
		rec := recipeRecord{ID: recipeID, Kind: recipeKind(in.SourceType), Origin: originUser}
		if err := insertRecipe(ctx, pgxTx, rec); err != nil {
			return err
		}
		ver := versionRecord{
			ID:                             versionID,
			RecipeID:                       recipeID,
			Version:                        1,
			SchemaVersion:                  schemaVersion,
			CatalogVersion:                 graph.CatalogVersion,
			CreationSource:                 sourceUserExtract,
			Title:                          title,
			Description:                    description,
			PayloadJSON:                    encoded,
			PayloadHash:                    hash,
			PreferredVisualSystemVersionID: preferred,
		}
		if err := insertVersion(ctx, pgxTx, ver); err != nil {
			return err
		}
		if err := setCurrentVersion(ctx, pgxTx, recipeID, versionID); err != nil {
			return err
		}
		loaded, err := loadRecipe(ctx, pgxTx, recipeID, false)
		if err != nil {
			return err
		}
		out, err = serializeRecipe(loaded)
		return err
	})
	return out, err
}

// Append 从当前 live 图再提取一版，接到已有配方。
// 调用时机：HTTP POST .../versions。已归档 Conflict；expected_recipe_version 对不上 Conflict。
// 不改目标商品的 live 图。
func (s Service) Append(ctx context.Context, in AppendInput) (RecipeView, error) {
	var out RecipeView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		rec, err := loadRecipe(ctx, pgxTx, in.RecipeID, true)
		if err != nil {
			return err
		}
		if rec.ArchivedAt != nil {
			return apperr.Conflict("已归档工作流配方不能追加版本")
		}
		if rec.Current == nil || rec.Current.Version != in.ExpectedRecipeVersion {
			return apperr.Conflict("工作流配方版本已变化，请刷新后重试")
		}
		payload, err := extractLive(ctx, pgxTx, in.CreateInput)
		if err != nil {
			return err
		}
		preferred, err := optionalVisual(ctx, pgxTx, in.PreferredVisualSystemVersionID)
		if err != nil {
			return err
		}
		title, description, err := normalizeTitle(in.Title, in.Description)
		if err != nil {
			return err
		}
		encoded, err := payloadJSON(payload)
		if err != nil {
			return err
		}
		hash, err := payloadHash(payload)
		if err != nil {
			return err
		}
		versionID := clockid.New()
		ver := versionRecord{
			ID:                             versionID,
			RecipeID:                       rec.ID,
			Version:                        rec.Current.Version + 1,
			SchemaVersion:                  schemaVersion,
			CatalogVersion:                 graph.CatalogVersion,
			CreationSource:                 sourceUserExtract,
			Title:                          title,
			Description:                    description,
			PayloadJSON:                    encoded,
			PayloadHash:                    hash,
			PreferredVisualSystemVersionID: preferred,
		}
		if err := insertVersion(ctx, pgxTx, ver); err != nil {
			return err
		}
		if err := setCurrentVersion(ctx, pgxTx, rec.ID, versionID); err != nil {
			return err
		}
		loaded, err := loadRecipe(ctx, pgxTx, rec.ID, false)
		if err != nil {
			return err
		}
		out, err = serializeRecipe(loaded)
		return err
	})
	return out, err
}

// Archive 归档配方，使其不再出现在默认列表。已归档则 Changed=false 幂等成功。
// 调用时机：HTTP DELETE /workflow-recipes/:recipe_id。版本对不上 Conflict。不删 versions 行。
func (s Service) Archive(ctx context.Context, recipeID string, expectedVersion int) (ArchiveView, error) {
	var out ArchiveView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		rec, err := loadRecipe(ctx, pgxTx, recipeID, true)
		if err != nil {
			return err
		}
		if rec.Current == nil || rec.Current.Version != expectedVersion {
			return apperr.Conflict("工作流配方版本已变化，请刷新后重试")
		}
		changed := rec.ArchivedAt == nil
		if changed {
			if err := archiveRecipeRow(ctx, pgxTx, rec.ID); err != nil {
				return err
			}
		}
		loaded, err := loadRecipe(ctx, pgxTx, rec.ID, false)
		if err != nil {
			return err
		}
		view, err := serializeRecipe(loaded)
		if err != nil {
			return err
		}
		out = ArchiveView{Changed: changed, Recipe: view}
		return nil
	})
	return out, err
}

// Preview 计算应用到目标商品时将出现的节点与边；完整配方不能 merge 进已有 live 图。
// 商品或配方不存在返回 NotFound；已归档、版本已变或完整配方遇上已有图返回 Conflict。
func (s Service) Preview(ctx context.Context, productID, recipeID string, expectedVersion int) (Preview, error) {
	ctx = graph.WithProductGuard(ctx, s.Products)
	var out Preview
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		target, err := getProductTarget(ctx, pgxTx, productID, false)
		if err != nil {
			return err
		}
		plan, err := s.planForProduct(ctx, pgxTx, target, recipeID, expectedVersion, nil)
		if err != nil {
			return err
		}
		out = plan.preview()
		return nil
	})
	return out, err
}

// Apply 按预览 digest 确认写入目标 live 图。完整配方在已有 live 图上返回冲突；fragment 可 merge 或返回显式冲突。相同幂等键回放。
func (s Service) Apply(ctx context.Context, in ApplyInput) (ApplicationResult, error) {
	ctx = graph.WithProductGuard(ctx, s.Products)
	key, err := normalizeIdempotencyKey(in.IdempotencyKey)
	if err != nil {
		return ApplicationResult{}, err
	}
	digest, err := normalizePreviewDigest(in.PreviewDigest)
	if err != nil {
		return ApplicationResult{}, err
	}
	requestHash, err := jsonHash(map[string]any{
		"product_id":              in.ProductID,
		"recipe_id":               in.RecipeID,
		"expected_recipe_version": in.ExpectedRecipeVersion,
		"expected_graph_revision": in.ExpectedGraphRevision,
		"preview_digest":          digest,
	})
	if err != nil {
		return ApplicationResult{}, err
	}
	var out ApplicationResult
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		target, err := getProductTarget(ctx, pgxTx, in.ProductID, true)
		if err != nil {
			return err
		}
		existing, err := applicationByKey(ctx, pgxTx, in.ProductID, key)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.RequestHash != requestHash {
				return apperr.Conflict("相同 idempotency key 不能应用不同的工作流配方")
			}
			out, err = s.applicationResult(ctx, pgxTx, *existing, false)
			return err
		}
		rec, err := loadRecipe(ctx, pgxTx, in.RecipeID, true)
		if err != nil {
			return err
		}
		live, err := graph.TryLiveForUpdate(ctx, pgxTx, in.ProductID)
		if err != nil {
			return err
		}
		expectedRev := in.ExpectedGraphRevision
		plan, err := s.planFromRecipe(target, rec, in.ExpectedRecipeVersion, live, &expectedRev)
		if err != nil {
			return err
		}
		if plan.PreviewDigest != digest {
			return apperr.Conflict("配方预览已变化，请重新预览后重试")
		}
		command, err := applyPlanCommand(ctx, pgxTx, in.ProductID, plan)
		if err != nil {
			return wrapMergeErr(err)
		}
		beforeNodes := map[string]struct{}{}
		for _, node := range plan.Existing.Nodes {
			beforeNodes[node.ID] = struct{}{}
		}
		beforeEdges := map[string]struct{}{}
		for _, edge := range plan.Existing.Edges {
			beforeEdges[edge.ID] = struct{}{}
		}
		addedNodes := []string{}
		for _, node := range command.Applied.Nodes {
			if _, ok := beforeNodes[node.ID]; !ok {
				addedNodes = append(addedNodes, node.ID)
			}
		}
		addedEdges := []string{}
		for _, edge := range command.Applied.Edges {
			if _, ok := beforeEdges[edge.ID]; !ok {
				addedEdges = append(addedEdges, edge.ID)
			}
		}
		appID := clockid.New()
		baseRev := plan.BaseGraphRevision
		digestCopy := plan.PreviewDigest
		appRow := applicationRecord{
			ID:                   appID,
			ProductID:            in.ProductID,
			RecipeVersionID:      rec.Current.ID,
			RecipeID:             rec.ID,
			RecipeVersion:        rec.Current.Version,
			GraphID:              command.GraphID,
			OperationGroupID:     command.OperationGroupID,
			Mode:                 plan.Mode,
			IdempotencyKey:       key,
			RequestHash:          requestHash,
			AddedNodeIDs:         addedNodes,
			AddedEdgeIDs:         addedEdges,
			PreviewGraphRevision: &baseRev,
			PreviewDigest:        &digestCopy,
			UpdatedNodeIDs:       []string{},
			RequiredBindings:     plan.RequiredBindings,
		}
		if err := insertApplication(ctx, pgxTx, appRow); err != nil {
			return err
		}
		out, err = s.applicationResult(ctx, pgxTx, appRow, true)
		return err
	})
	if applicationKeyConflict(err) {
		replay, loadErr := s.loadReplay(ctx, in.ProductID, key, requestHash)
		if loadErr != nil {
			return ApplicationResult{}, loadErr
		}
		return replay, nil
	}
	return out, err
}

func (s Service) loadReplay(ctx context.Context, productID, key, requestHash string) (ApplicationResult, error) {
	var out ApplicationResult
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		existing, err := applicationByKey(ctx, pgxTx, productID, key)
		if err != nil {
			return err
		}
		if existing == nil {
			return apperr.Conflict("相同 idempotency key 不能应用不同的工作流配方")
		}
		if existing.RequestHash != requestHash {
			return apperr.Conflict("相同 idempotency key 不能应用不同的工作流配方")
		}
		out, err = s.applicationResult(ctx, pgxTx, *existing, false)
		return err
	})
	return out, err
}

func (s Service) planForProduct(
	ctx context.Context,
	pgxTx *gorm.DB,
	target productTarget,
	recipeID string,
	expectedVersion int,
	expectedGraphRevision *int,
) (applyPlan, error) {
	rec, err := loadRecipe(ctx, pgxTx, recipeID, false)
	if err != nil {
		return applyPlan{}, err
	}
	live, err := graph.TryLive(ctx, pgxTx, target.ID)
	if err != nil {
		return applyPlan{}, err
	}
	return s.planFromRecipe(target, rec, expectedVersion, live, expectedGraphRevision)
}

// planFromRecipe 从已加载的配方行与 live 快照算出 applyPlan。预览纯计算；确认写入走 applyPlanCommand。
// 已归档或 current 版本对不上 expectedVersion 返回 409。目标没有 live 图时 existing 是空图，后面走 create。
func (s Service) planFromRecipe(
	target productTarget,
	rec recipeRecord,
	expectedVersion int,
	live *graph.Live,
	expectedGraphRevision *int,
) (applyPlan, error) {
	if rec.ArchivedAt != nil {
		return applyPlan{}, apperr.Conflict("已归档工作流配方不能应用")
	}
	if rec.Current == nil || rec.Current.Version != expectedVersion {
		return applyPlan{}, apperr.Conflict("工作流配方版本已变化，请刷新后重试")
	}
	payload, err := parsePayloadOrRaise(rec.Current.PayloadJSON, rec.Current.PayloadHash)
	if err != nil {
		return applyPlan{}, err
	}
	required, err := parseGovernance(rec.Current.GovernanceJSON)
	if err != nil {
		return applyPlan{}, err
	}
	var identity *graph.Identity
	existing := graph.EmptyGraph
	if live != nil {
		id := live.Identity
		identity = &id
		existing = live.Applied
	}
	return planPayload(
		target,
		identity,
		existing,
		rec.ID,
		rec.Kind,
		rec.Current.Version,
		applicationSummary(rec.Current.Title),
		payload,
		rec.Current.PayloadHash,
		required,
		expectedGraphRevision,
	)
}

// applyPlanCommand 把已确认的 change intent 交给 Graph Command。
// create 走新建；merge 要求当前 active 图 id 仍是预览时的 GraphID，否则 409 重新预览。
func applyPlanCommand(ctx context.Context, pgxTx *gorm.DB, productID string, plan applyPlan) (graph.CommandResult, error) {
	cmd := graph.Command{
		ProductID: productID,
		ChangeSet: plan.ChangeSet,
		Kind:      graph.HistoryEdit,
	}
	if plan.Mode == ModeCreate {
		title := limitRunes(plan.ChangeSet.Summary, 255)
		if title == "" {
			title = graph.DefaultGraphTitle
		}
		cmd.Title = title
		return graph.WriteTx(ctx, pgxTx, cmd)
	}
	cmd.GraphID = plan.GraphID
	cmd.RequireActive = true
	return graph.WriteTx(ctx, pgxTx, cmd)
}

// applicationResult 在同一事务里投影刚写入的图。Created 表示这次是新确认还是幂等回放。
func (s Service) applicationResult(ctx context.Context, pgxTx *gorm.DB, rec applicationRecord, created bool) (ApplicationResult, error) {
	proj, err := graph.ProjectGraph(ctx, pgxTx, rec.ProductID, rec.GraphID)
	if err != nil {
		return ApplicationResult{}, err
	}
	return ApplicationResult{
		Created:           created,
		RecipeID:          rec.RecipeID,
		RecipeVersionID:   rec.RecipeVersionID,
		RecipeVersion:     rec.RecipeVersion,
		Mode:              rec.Mode,
		Graph:             proj,
		AddedNodeIDs:      nonNilStrings(rec.AddedNodeIDs),
		AddedEdgeIDs:      nonNilStrings(rec.AddedEdgeIDs),
		UpdatedNodeIDs:    nonNilStrings(rec.UpdatedNodeIDs),
		BaseGraphRevision: rec.PreviewGraphRevision,
		PreviewDigest:     rec.PreviewDigest,
		RequiredBindings:  nonNilStrings(rec.RequiredBindings),
	}, nil
}

// extractLive FOR UPDATE 锁 live 图再提取。非 active 或 revision 对不上返回 409，避免从过期画布存配方。
func extractLive(ctx context.Context, pgxTx *gorm.DB, in CreateInput) (Payload, error) {
	live, err := graph.LoadLiveForUpdate(ctx, pgxTx, in.ProductID, in.WorkflowID, in.ExpectedGraphRevision)
	if err != nil {
		return Payload{}, err
	}
	return extractPayload(live.Applied, ExtractInput{
		SourceType: in.SourceType,
		GroupID:    in.GroupID,
		NodeIDs:    in.NodeIDs,
	})
}

func optionalVisual(ctx context.Context, tx *gorm.DB, id *string) (*string, error) {
	if id == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*id)
	if trimmed == "" {
		return nil, nil
	}
	if err := visualSystemVersionExists(ctx, tx, trimmed); err != nil {
		return nil, err
	}
	return &trimmed, nil
}

func normalizeTitle(title string, description *string) (string, *string, error) {
	normalized := strings.TrimSpace(title)
	if normalized == "" {
		return "", nil, apperr.Validation("配方名称不能为空")
	}
	var desc *string
	if description != nil {
		trimmed := strings.TrimSpace(*description)
		if trimmed != "" {
			desc = &trimmed
		}
	}
	return normalized, desc, nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("idempotency key 不能为空")
	}
	if len([]rune(normalized)) > 120 {
		return "", apperr.Validation("idempotency key 不能超过 120 个字符")
	}
	return normalized, nil
}

func normalizePreviewDigest(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != 64 {
		return "", apperr.Validation("preview digest 必须是 64 位十六进制字符串")
	}
	for _, c := range normalized {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", apperr.Validation("preview digest 必须是 64 位十六进制字符串")
		}
	}
	return normalized, nil
}
