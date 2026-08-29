package recipe

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

type Service struct {
	Pool *pgxpool.Pool
}

type CreateInput struct {
	ProductID                      string
	WorkflowID                     string
	SourceType                     string
	GroupID                        *string
	NodeIDs                        []string
	ExpectedGraphRevision          int
	Title                          string
	Description                    *string
	PreferredVisualSystemVersionID *string
}

type AppendInput struct {
	CreateInput
	RecipeID              string
	ExpectedRecipeVersion int
}

type ApplyInput struct {
	ProductID             string
	RecipeID              string
	ExpectedRecipeVersion int
	ExpectedGraphRevision int
	PreviewDigest         string
	IdempotencyKey        string
}

type ApplicationResult struct {
	Created           bool
	RecipeID          string
	RecipeVersionID   string
	RecipeVersion     int
	Mode              string
	Graph             graph.Projection
	AddedNodeIDs      []string
	AddedEdgeIDs      []string
	UpdatedNodeIDs    []string
	BaseGraphRevision *int
	PreviewDigest     *string
	RequiredBindings  []string
}

func (s Service) List(ctx context.Context, includeArchived bool) ([]RecipeView, error) {
	var out []RecipeView
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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

func (s Service) Get(ctx context.Context, recipeID string) (RecipeView, error) {
	var out RecipeView
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		rec, err := loadRecipe(ctx, pgxTx, recipeID, false)
		if err != nil {
			return err
		}
		out, err = serializeRecipe(rec)
		return err
	})
	return out, err
}

func (s Service) Create(ctx context.Context, in CreateInput) (RecipeView, error) {
	var out RecipeView
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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

func (s Service) Append(ctx context.Context, in AppendInput) (RecipeView, error) {
	var out RecipeView
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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

func (s Service) Archive(ctx context.Context, recipeID string, expectedVersion int) (ArchiveView, error) {
	var out ArchiveView
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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

func (s Service) Preview(ctx context.Context, productID, recipeID string, expectedVersion int) (Preview, error) {
	var out Preview
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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

func (s Service) Apply(ctx context.Context, in ApplyInput) (ApplicationResult, error) {
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
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
		if _, err := graph.LoadActiveGraphForUpdate(ctx, pgxTx, in.ProductID); err != nil {
			return err
		}
		expectedRev := in.ExpectedGraphRevision
		plan, err := s.planFromRecipe(ctx, pgxTx, target, rec, in.ExpectedRecipeVersion, &expectedRev)
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
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
	pgxTx pgx.Tx,
	target productTarget,
	recipeID string,
	expectedVersion int,
	expectedGraphRevision *int,
) (applyPlan, error) {
	rec, err := loadRecipe(ctx, pgxTx, recipeID, false)
	if err != nil {
		return applyPlan{}, err
	}
	return s.planFromRecipe(ctx, pgxTx, target, rec, expectedVersion, expectedGraphRevision)
}

func (s Service) planFromRecipe(
	ctx context.Context,
	pgxTx pgx.Tx,
	target productTarget,
	rec recipeRecord,
	expectedVersion int,
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
	live, err := graph.TryLoadActiveGraph(ctx, pgxTx, target.ID)
	if err != nil {
		return applyPlan{}, err
	}
	existing := graph.EmptyGraph
	if live != nil {
		existing, err = graph.LoadAppliedGraph(ctx, pgxTx, *live)
		if err != nil {
			return applyPlan{}, err
		}
	}
	return planPayload(
		target,
		live,
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

func applyPlanCommand(ctx context.Context, pgxTx pgx.Tx, productID string, plan applyPlan) (graph.CommandResult, error) {
	if plan.Mode == ModeCreate {
		title := limitRunes(plan.ChangeSet.Summary, 255)
		if title == "" {
			title = graph.DefaultGraphTitle
		}
		return graph.StageNew(ctx, pgxTx, productID, title, plan.ChangeSet)
	}
	live, err := graph.TryLoadActiveGraph(ctx, pgxTx, productID)
	if err != nil {
		return graph.CommandResult{}, err
	}
	if live == nil {
		return graph.CommandResult{}, apperr.Conflict("商品没有可写入的 schema-v3 工作流")
	}
	if plan.GraphID == nil || *plan.GraphID != live.ID {
		return graph.CommandResult{}, apperr.Conflict("工作流已变化，请重新预览后重试")
	}
	return graph.Mutate(ctx, pgxTx, productID, live.ID, plan.ChangeSet, graph.HistoryEdit)
}

func (s Service) applicationResult(ctx context.Context, pgxTx pgx.Tx, rec applicationRecord, created bool) (ApplicationResult, error) {
	row, err := graph.LoadGraph(ctx, pgxTx, rec.ProductID, rec.GraphID)
	if err != nil {
		return ApplicationResult{}, err
	}
	proj, err := graph.Project(ctx, pgxTx, row)
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

func extractLive(ctx context.Context, pgxTx pgx.Tx, in CreateInput) (Payload, error) {
	row, err := graph.LoadGraphForUpdate(ctx, pgxTx, in.ProductID, in.WorkflowID)
	if err != nil {
		return Payload{}, err
	}
	if !row.Active {
		return Payload{}, apperr.Conflict("只能从 active schema-v3 工作流保存配方")
	}
	if row.Revision != in.ExpectedGraphRevision {
		return Payload{}, apperr.Conflict("工作流已变化，请刷新后重试")
	}
	applied, err := graph.LoadAppliedGraph(ctx, pgxTx, row)
	if err != nil {
		return Payload{}, err
	}
	return extractPayload(applied, ExtractInput{
		SourceType: in.SourceType,
		GroupID:    in.GroupID,
		NodeIDs:    in.NodeIDs,
	})
}

func optionalVisual(ctx context.Context, tx pgx.Tx, id *string) (*string, error) {
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
