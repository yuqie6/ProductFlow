package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type classifiedNodeError struct{ error }

func (e classifiedNodeError) Unwrap() error { return e.error }

func (e Executor) executeClaimedNode(ctx context.Context, runID, nodeRunID string) error {
	err := e.runClaimedNode(ctx, runID, nodeRunID)
	if err == nil || isProviderUnknown(err) {
		return err
	}
	var classified classifiedNodeError
	if errors.As(err, &classified) {
		return nil
	}
	var app apperr.Error
	reason := err.Error()
	if errors.As(err, &app) {
		reason = app.Detail
	}
	if reason == "" {
		reason = "节点运行失败"
	}
	if failErr := failClaimedNode(ctx, e.DB, runID, nodeRunID, reason); failErr != nil {
		return failErr
	}
	return nil
}

var errProviderFenced = errors.New("graph provider call fenced")

func (e Executor) runClaimedNode(ctx context.Context, runID, nodeRunID string) error {
	run, err := e.loadRun(ctx, runID)
	if err != nil {
		return err
	}
	var nodeRun *graphNodeRunRow
	for i := range run.NodeRuns {
		if run.NodeRuns[i].ID == nodeRunID {
			nodeRun = &run.NodeRuns[i]
			break
		}
	}
	if nodeRun == nil || run.Status != RunStatusRunning || nodeRun.Status != NodeRunRunning {
		return nil
	}
	e.logger().Info("graph node run",
		zap.String("workflow_run_id", runID),
		zap.String("workflow_node_run_id", nodeRunID),
	)
	if nodeRun.NodeID == nil {
		return apperr.Validation("运行节点已从当前图中删除")
	}
	applied, err := appliedGraphFromSnapshot(run.Snapshot)
	if err != nil {
		return err
	}
	sources := sourcesFromSnapshot(run.Snapshot)
	sources, err = hydrateSourcesFromNodeRuns(ctx, e.DB, run.NodeRuns, sources)
	if err != nil {
		return err
	}
	node, err := applied.Node(*nodeRun.NodeID)
	if err != nil {
		return err
	}
	digest, err := compileInputDigest(applied, node.ID, sources)
	if err != nil {
		return err
	}
	if err := writeCompiledContext(ctx, e.DB, nodeRun.ID, node, applied, sources, digest); err != nil {
		return err
	}
	if skipped, err := e.skipUnchanged(ctx, run, *nodeRun, sources, digest); err != nil || skipped {
		return err
	}
	prompt := e.Deps.Prompt
	if prompt == nil {
		prompt = MockPromptProvider{}
	}
	image := e.Deps.Image
	if image == nil {
		image = MockImageProvider{}
	}
	facts, briefs, visual, promptRefs, err := collectPromptInputs(applied, node.ID, sources)
	if err != nil {
		return err
	}
	loadedRefs, err := e.loadReferences(ctx, promptRefs)
	if err != nil {
		return err
	}
	req, err := AssemblePromptRequest(node, facts, briefs, visual, loadedRefs, digest)
	if err != nil {
		return err
	}
	switch node.NodeType {
	case NodeCreativeBrief:
		result, promote, err := e.callProvider(ctx, run.ID, *nodeRun, prompt.Name(), digest, node.NodeType, func() (PromptResult, error) {
			return prompt.GenerateCreativeBrief(ctx, req)
		})
		if errors.Is(err, errProviderFenced) {
			return nil
		}
		if err != nil {
			return err
		}
		if result.Payload == nil {
			return fmt.Errorf("提示词 provider 未返回结构化输出")
		}
		return e.persistContentArtifact(ctx, run, *nodeRun, "creative_brief", result, digest, promote, prompt.Name(), func(config map[string]any) map[string]any {
			out := cloneMap(config)
			for key, value := range result.Payload {
				out[key] = value
			}
			return out
		})
	case NodeVisualSystem:
		result, promote, err := e.callProvider(ctx, run.ID, *nodeRun, prompt.Name(), digest, node.NodeType, func() (PromptResult, error) {
			return prompt.GenerateVisualOverlay(ctx, req)
		})
		if errors.Is(err, errProviderFenced) {
			return nil
		}
		if err != nil {
			return err
		}
		if result.Payload == nil {
			return fmt.Errorf("提示词 provider 未返回结构化输出")
		}
		return e.persistContentArtifact(ctx, run, *nodeRun, "visual_system", result, digest, promote, prompt.Name(), func(config map[string]any) map[string]any {
			out := cloneMap(config)
			out["visual_overlay"] = result.Payload
			return out
		})
	case NodePromptGeneration:
		result, promote, err := e.callProvider(ctx, run.ID, *nodeRun, prompt.Name(), digest, node.NodeType, func() (PromptResult, error) {
			return prompt.GeneratePrompt(ctx, req)
		})
		if errors.Is(err, errProviderFenced) {
			return nil
		}
		if err != nil {
			return err
		}
		if result.Payload == nil {
			return fmt.Errorf("提示词 provider 未返回结构化输出")
		}
		storedPrompt, _ := node.Config["prompt"].(map[string]any)
		result.Payload = ApplyTextPolicyToPrompt(
			stripV3PromptPayload(result.Payload),
			req.TextPolicy,
			promptConfigHasAuthoredText(storedPrompt),
		)
		return e.persistContentArtifact(ctx, run, *nodeRun, "prompt", result, digest, promote, prompt.Name(), func(config map[string]any) map[string]any {
			out := cloneMap(config)
			out["prompt"] = result.Payload
			return out
		})
	case NodeImageGeneration:
		imgReq := ImageRequest{NodeTitle: node.Title, InputDigest: digest, GenerationSpec: map[string]any{}}
		if spec, ok := node.Config["generation_spec"].(map[string]any); ok {
			imgReq.GenerationSpec = spec
		}
		if key, ok := node.Config["image_type_key"].(string); ok {
			imgReq.ImageTypeKey = key
		}
		promptPayload, artifactID, err := incomingPromptArtifact(applied, node.ID, sources)
		if err != nil {
			return err
		}
		imgReq.Prompt = promptPayload
		imgReq.PromptArtifactID = artifactID
		if stored, ok := node.Config["prompt"].(map[string]any); ok && len(imgReq.Prompt) == 0 {
			imgReq.Prompt = stored
		}
		imgReq.References = loadedRefs
		imgReq.VisualSystem = visual
		imgReq.VisualOverlay = visualOverlayFromConfig(node.Config)
		if variation, ok := node.Config["variation_instruction"].(string); ok {
			imgReq.VariationInstruction = variation
		}
		for _, edge := range incomingSorted(applied, node.ID) {
			imgReq.IncomingEdgeIDs = append(imgReq.IncomingEdgeIDs, edge.ID)
		}
		img, promote, err := e.callImageProvider(ctx, run.ID, *nodeRun, image.Name(), digest, node.NodeType, func() (ImageResult, error) {
			return image.GenerateImage(ctx, imgReq)
		})
		if errors.Is(err, errProviderFenced) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(img.Bytes) == 0 {
			return fmt.Errorf("图片 provider 未返回图片结果")
		}
		return e.persistImageArtifact(ctx, run, *nodeRun, node, img, digest, promote, image.Name(), imgReq.PromptArtifactID)
	default:
		return apperr.Validation("不能运行该节点类型")
	}
}

func (e Executor) skipUnchanged(ctx context.Context, run graphRunRow, nodeRun graphNodeRunRow, sources map[string]SourceRecord, digest string) (bool, error) {
	if run.RunScope != RunScopeGraph || nodeRun.NodeID == nil {
		return false, nil
	}
	record := sources[*nodeRun.NodeID]
	if record.CurrentArtifactID == nil || record.CurrentInputDigest == nil || *record.CurrentInputDigest != digest {
		return false, nil
	}
	now := time.Now().UTC()
	skippedOut := map[string]any{"artifact_id": *record.CurrentArtifactID, "skipped": true}
	if record.CurrentOutputAssetID != nil && *record.CurrentOutputAssetID != "" {
		skippedOut["product_image_asset_id"] = *record.CurrentOutputAssetID
	}
	output, _ := json.Marshal(skippedOut)
	return true, tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		_, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE workflow_graph_node_runs SET
				status = 'succeeded', finished_at = $2, active_attempt_id = NULL, output_json = $3
			WHERE id = $1
		`, nodeRun.ID, now, output)
		return err
	})
}

func (e Executor) callProvider(
	ctx context.Context,
	runID string,
	nodeRun graphNodeRunRow,
	providerName, digest string,
	nodeType NodeType,
	invoke func() (PromptResult, error),
) (PromptResult, bool, error) {
	if nodeRun.ActiveAttemptID == nil || *nodeRun.ActiveAttemptID == "" {
		return PromptResult{}, false, apperr.Validation("节点运行缺少 attempt token")
	}
	attemptID := *nodeRun.ActiveAttemptID
	request := map[string]any{
		"node_id":      nodeRun.NodeID,
		"node_type":    string(nodeType),
		"input_digest": digest,
		"attempt_id":   attemptID,
	}
	if err := e.prepareProviderCall(ctx, nodeRun.ID, attemptID, providerName, request); err != nil {
		return PromptResult{}, false, err
	}
	result, err := invoke()
	if err != nil {
		if markErr := e.markUnknownCommitted(ctx, runID, nodeRun.ID, &attemptID); markErr != nil {
			return PromptResult{}, false, markErr
		}
		return PromptResult{}, false, providerUnknownError{}
	}
	promote, err := e.finishProviderCall(ctx, runID, nodeRun.ID, attemptID, map[string]any{
		"model":       result.Model,
		"response_id": result.ResponseID,
	})
	return result, promote, err
}

func (e Executor) callImageProvider(
	ctx context.Context,
	runID string,
	nodeRun graphNodeRunRow,
	providerName, digest string,
	nodeType NodeType,
	invoke func() (ImageResult, error),
) (ImageResult, bool, error) {
	if nodeRun.ActiveAttemptID == nil || *nodeRun.ActiveAttemptID == "" {
		return ImageResult{}, false, apperr.Validation("节点运行缺少 attempt token")
	}
	attemptID := *nodeRun.ActiveAttemptID
	request := map[string]any{
		"node_id":      nodeRun.NodeID,
		"node_type":    string(nodeType),
		"input_digest": digest,
		"attempt_id":   attemptID,
	}
	if err := e.prepareProviderCall(ctx, nodeRun.ID, attemptID, providerName, request); err != nil {
		return ImageResult{}, false, err
	}
	result, err := invoke()
	if err != nil {
		if markErr := e.markUnknownCommitted(ctx, runID, nodeRun.ID, &attemptID); markErr != nil {
			return ImageResult{}, false, markErr
		}
		return ImageResult{}, false, providerUnknownError{}
	}
	promote, err := e.finishProviderCall(ctx, runID, nodeRun.ID, attemptID, map[string]any{
		"model":           result.Model,
		"response_id":     result.ResponseID,
		"provider_status": result.ProviderStatus,
	})
	return result, promote, err
}

func (e Executor) prepareProviderCall(ctx context.Context, nodeRunID, attemptID, providerName string, request map[string]any) error {
	hash, err := providerEffectHash(request)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(request)
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		ok, err := advanceNodePhase(ctx, pgxTx, nodeRunID, attemptID, "prepared")
		if err != nil {
			return err
		}
		if !ok {
			return errProviderFenced
		}
		ok, err = ensureProviderEffectIntent(ctx, pgxTx, nodeRunID, attemptID, hash, providerName, raw)
		if err != nil {
			return err
		}
		if !ok {
			return errProviderFenced
		}
		ok, err = advanceNodePhase(ctx, pgxTx, nodeRunID, attemptID, "provider_call")
		if err != nil {
			return err
		}
		if !ok {
			return errProviderFenced
		}
		return nil
	})
}

func (e Executor) finishProviderCall(ctx context.Context, runID, nodeRunID, attemptID string, resultJSON map[string]any) (bool, error) {
	var promote bool
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		ok, err := recordProviderEffectResult(ctx, pgxTx, nodeRunID, attemptID, resultJSON)
		if err != nil {
			return err
		}
		if !ok {
			promote = false
			return nil
		}
		ok, err = advanceNodePhase(ctx, pgxTx, nodeRunID, attemptID, "provider_result_received")
		if err != nil {
			return err
		}
		if !ok {
			promote = false
			return nil
		}
		var runStatus, nodeStatus string
		var active *string
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus); err != nil {
			return err
		}
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT status, active_attempt_id FROM workflow_graph_node_runs WHERE id = $1 FOR UPDATE`, nodeRunID).Scan(&nodeStatus, &active); err != nil {
			return err
		}
		promote = runStatus == RunStatusRunning && nodeStatus == NodeRunRunning && active != nil && *active == attemptID
		return nil
	})
	return promote, err
}

func (e Executor) markUnknownCommitted(ctx context.Context, runID, nodeRunID string, attemptID *string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		if err := markNodeUnknown(ctx, pgxTx, runID, nodeRunID, attemptID, ProviderUnknownDetail); err != nil {
			return err
		}
		_, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
		return err
	})
}

func (e Executor) markFailedCommitted(ctx context.Context, runID, nodeRunID string, attemptID *string, detail string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		if err := markNodeFailed(ctx, pgxTx, runID, nodeRunID, attemptID, detail); err != nil {
			return err
		}
		_, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
		return err
	})
}

func (e Executor) persistContentArtifact(
	ctx context.Context,
	run graphRunRow,
	nodeRun graphNodeRunRow,
	artifactType string,
	result PromptResult,
	digest string,
	promote bool,
	providerName string,
	writeback func(map[string]any) map[string]any,
) error {
	if err := validateGeneratedPayload(artifactType, result.Payload); err != nil {
		return err
	}
	payload, err := json.Marshal(result.Payload)
	if err != nil {
		return err
	}
	hash := sha256Hex(payload)
	now := time.Now().UTC()
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		artifactID, err := upsertArtifact(ctx, pgxTx, run, nodeRun, artifactType, payload, hash, digest, providerName, result.Model, nil)
		if err != nil {
			return err
		}
		if !promote {
			return nil
		}
		if promote && nodeRun.NodeID != nil {
			var liveRevision int
			if err := pfdb.QueryRow(ctx, pgxTx, `SELECT revision FROM workflow_graphs WHERE id = $1`, run.GraphID).Scan(&liveRevision); err != nil {
				return err
			}
			if liveRevision == run.GraphRevision {
				if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2`, artifactID, *nodeRun.NodeID); err != nil {
					return err
				}
				if writeback != nil {
					var configJSON []byte
					if err := pfdb.QueryRow(ctx, pgxTx, `SELECT config_json FROM workflow_graph_nodes WHERE id = $1`, *nodeRun.NodeID).Scan(&configJSON); err != nil {
						return err
					}
					config := map[string]any{}
					_ = json.Unmarshal(configJSON, &config)
					updated, err := json.Marshal(writeback(config))
					if err != nil {
						return err
					}
					if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE workflow_graph_nodes SET config_json = $2 WHERE id = $1`, *nodeRun.NodeID, updated); err != nil {
						return err
					}
				}
			}
		}
		output, _ := json.Marshal(map[string]any{"artifact_id": artifactID})
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE workflow_graph_node_runs SET
				status = 'succeeded', finished_at = $2, active_attempt_id = NULL, output_json = $3
			WHERE id = $1
		`, nodeRun.ID, now, output)
		if err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID)
		return err
	})
}

func (e Executor) persistImageArtifact(
	ctx context.Context,
	run graphRunRow,
	nodeRun graphNodeRunRow,
	node AppliedNode,
	img ImageResult,
	digest string,
	promote bool,
	providerName, promptArtifactID string,
) error {
	if e.Deps.Assets == nil {
		return apperr.Validation("节点运行失败")
	}
	var productID string
	if err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		id, err := loadProductIDForGraph(ctx, pgxTx, run.GraphID)
		productID = id
		return err
	}); err != nil {
		return err
	}
	var imageTypeKey *string
	if key, ok := node.Config["image_type_key"].(string); ok && key != "" {
		imageTypeKey = &key
	}
	now := time.Now().UTC()
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		assetID, err := e.Deps.Assets.Write(ctx, pgxTx, GeneratedImageInput{
			ProductID:    productID,
			Title:        node.Title,
			Filename:     node.Title + ".png",
			Bytes:        img.Bytes,
			MIME:         img.MIME,
			ImageTypeKey: imageTypeKey,
		})
		if err != nil {
			return err
		}
		width, height := img.Width, img.Height
		if width <= 0 || height <= 0 {
			if verified, inspectErr := media.Inspect(img.Bytes, img.MIME); inspectErr == nil {
				width, height = verified.Width, verified.Height
			}
		}
		if width <= 0 || height <= 0 {
			return apperr.Validation("供应商没有返回可读取的图片")
		}
		spec, _ := node.Config["generation_spec"].(map[string]any)
		if spec == nil {
			spec = map[string]any{}
		}
		requestedAspect, _ := spec["aspect_ratio"].(string)
		matched := measuredAspectMatches(requestedAspect, width, height)
		measured := map[string]any{
			"mime_type":              img.MIME,
			"provider_status":        img.ProviderStatus,
			"byte_size":              len(img.Bytes),
			"width":                  width,
			"height":                 height,
			"measured_width":         width,
			"measured_height":        height,
			"requested_aspect_ratio": requestedAspect,
			"requested_quality":      spec["quality_intent"],
			"aspect_matched":         matched,
			"aspect_mismatch":        nil,
		}
		if !matched {
			measured["aspect_mismatch"] = fmt.Sprintf("供应商没有按 %s 出图（实际 %d×%d）", requestedAspect, width, height)
		}
		if img.EffectiveParameters != nil {
			measured["effective_parameters"] = img.EffectiveParameters
		}
		payloadMap := map[string]any{
			"schema_version":         3,
			"product_image_asset_id": assetID,
			"generation_spec":        spec,
			"prompt_artifact_id":     emptyToNil(promptArtifactID),
			"measured_output":        measured,
		}
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return err
		}
		hash := sha256Hex(payload)
		artifactID, err := upsertArtifact(ctx, pgxTx, run, nodeRun, "image", payload, hash, digest, providerName, img.Model, &assetID)
		if err != nil {
			return err
		}
		if !promote {
			return nil
		}
		if nodeRun.NodeID != nil {
			var liveRevision int
			if err := pfdb.QueryRow(ctx, pgxTx, `SELECT revision FROM workflow_graphs WHERE id = $1`, run.GraphID).Scan(&liveRevision); err != nil {
				return err
			}
			if liveRevision == run.GraphRevision {
				if _, err := pfdb.Exec(ctx, pgxTx, `UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2`, artifactID, *nodeRun.NodeID); err != nil {
					return err
				}
			}
			if e.Deps.Delivery != nil {
				if err := e.Deps.Delivery.QueueAfterImageSuccess(ctx, pgxTx, *nodeRun.NodeID, assetID); err != nil {
					e.logger().Warn("image success kept; delivery rendition queue failed",
						zap.String("node_id", *nodeRun.NodeID), zap.Error(err))
				}
			}
		}
		output, _ := json.Marshal(map[string]any{
			"artifact_id":            artifactID,
			"product_image_asset_id": assetID,
		})
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE workflow_graph_node_runs SET
				status = 'succeeded', finished_at = $2, active_attempt_id = NULL, output_json = $3
			WHERE id = $1
		`, nodeRun.ID, now, output); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID)
		return err
	})
}

func (e Executor) loadReferences(ctx context.Context, refs []compiledReference) ([]ReferenceImage, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if e.Deps.Assets == nil {
		return nil, apperr.Validation("参考图不属于该商品")
	}
	out := make([]ReferenceImage, 0, len(refs))
	for _, ref := range refs {
		data, mime, filename, err := e.Deps.Assets.ReadAssetBytes(ctx, e.DB, ref.AssetID)
		if err != nil {
			return nil, err
		}
		if mime == "" && ref.MIMEType != nil {
			mime = *ref.MIMEType
		}
		switch mime {
		case "image/png", "image/jpeg", "image/webp":
		default:
			return nil, apperr.Validation("参考图仅支持 PNG、JPEG 或 WEBP")
		}
		if filename == "" {
			filename = ref.Label
		}
		out = append(out, ReferenceImage{
			AssetID: ref.AssetID, Role: ref.Role, Label: ref.Label,
			MIME: mime, Filename: filename, Bytes: data, EdgeID: ref.EdgeID,
		})
	}
	return out, nil
}

func upsertArtifact(
	ctx context.Context,
	tx *gorm.DB,
	run graphRunRow,
	nodeRun graphNodeRunRow,
	artifactType string,
	payload []byte,
	payloadHash, digest, providerName, model string,
	assetID *string,
) (string, error) {
	if providerName == "" {
		providerName = "unconfigured"
	}
	var existing string
	err := pfdb.QueryRow(ctx, tx, `SELECT id FROM workflow_graph_artifacts WHERE node_run_id = $1`, nodeRun.ID).Scan(&existing)
	if err == nil {
		_, err = pfdb.Exec(ctx, tx, `
			UPDATE workflow_graph_artifacts SET
				artifact_type = $2, schema_version = 3, graph_revision = $3,
				payload_json = $4, payload_hash = $5, input_digest = $6,
				product_image_asset_id = $7, provider_name = $8, provider_model = $9
			WHERE id = $1
		`, existing, artifactType, run.GraphRevision, payload, payloadHash, digest, assetID, providerName, model)
		return existing, err
	}
	if !errors.Is(err, sqldb.ErrNoRows) {
		return "", err
	}
	id := clockid.New()
	_, err = pfdb.Exec(ctx, tx, `
		INSERT INTO workflow_graph_artifacts (
			id, graph_id, node_id, node_run_id, artifact_type, schema_version, graph_revision,
			payload_json, payload_hash, input_digest, product_image_asset_id, provider_name, provider_model, created_at
		) VALUES ($1, $2, $3, $4, $5, 3, $6, $7, $8, $9, $10, $11, $12, NOW())
	`, id, run.GraphID, nodeRun.NodeID, nodeRun.ID, artifactType, run.GraphRevision, payload, payloadHash, digest, assetID, providerName, model)
	return id, err
}

func measuredAspectMatches(aspect string, width, height int) bool {
	if width <= 0 || height <= 0 || !strings.Contains(aspect, ":") {
		return false
	}
	parts := strings.SplitN(aspect, ":", 2)
	w, errW := strconv.ParseFloat(parts[0], 64)
	h, errH := strconv.ParseFloat(parts[1], 64)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return false
	}
	requested := w / h
	actual := float64(width) / float64(height)
	return absFloat(actual-requested) <= requested*0.08
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func sha256Hex(payload []byte) string {
	var decoded any
	if json.Unmarshal(payload, &decoded) == nil {
		if compact, err := marshalCompactSorted(decoded); err == nil {
			payload = compact
		}
	}
	return hashBytes(payload)
}

func hashBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func hydrateSourcesFromNodeRuns(ctx context.Context, pool *gorm.DB, nodeRuns []graphNodeRunRow, sources map[string]SourceRecord) (map[string]SourceRecord, error) {
	var ids []string
	for _, item := range nodeRuns {
		if item.Status == NodeRunSucceeded {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return sources, nil
	}
	rows, err := pfdb.Query(ctx, pool, `
		SELECT node_run_id, id, artifact_type, payload_json, input_digest, product_image_asset_id
		FROM workflow_graph_artifacts
		WHERE node_run_id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byNodeRun := map[string]SourceRecord{}
	for rows.Next() {
		var nodeRunID, artifactID, artifactType, digest string
		var payload []byte
		var assetID *string
		if err := rows.Scan(&nodeRunID, &artifactID, &artifactType, &payload, &digest, &assetID); err != nil {
			return nil, err
		}
		payloadMap := map[string]any{}
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &payloadMap); err != nil {
				return nil, err
			}
		}
		rec := SourceRecord{
			CurrentArtifactID:      &artifactID,
			CurrentArtifactType:    &artifactType,
			CurrentArtifactPayload: payloadMap,
			CurrentInputDigest:     &digest,
			CurrentOutputAssetID:   assetID,
		}
		switch artifactType {
		case "creative_brief":
			rec.Brief = cloneMap(payloadMap)
		case "visual_system":
			if overlay, ok := payloadMap["visual_overlay"].(map[string]any); ok {
				rec.VisualPayload = cloneMap(overlay)
			} else {
				rec.VisualPayload = cloneMap(payloadMap)
			}
		}
		byNodeRun[nodeRunID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, item := range nodeRuns {
		if item.NodeID == nil {
			continue
		}
		rec, ok := byNodeRun[item.ID]
		if !ok {
			continue
		}
		existing := sources[*item.NodeID]
		existing.CurrentArtifactID = rec.CurrentArtifactID
		existing.CurrentArtifactType = rec.CurrentArtifactType
		existing.CurrentArtifactPayload = rec.CurrentArtifactPayload
		existing.CurrentInputDigest = rec.CurrentInputDigest
		existing.CurrentOutputAssetID = rec.CurrentOutputAssetID
		if rec.Brief != nil {
			existing.Brief = rec.Brief
		}
		if rec.VisualPayload != nil {
			existing.VisualPayload = rec.VisualPayload
		}
		sources[*item.NodeID] = existing
	}
	return sources, nil
}

func writeCompiledContext(ctx context.Context, db *gorm.DB, nodeRunID string, node AppliedNode, applied AppliedGraph, sources map[string]SourceRecord, digest string) error {
	trace := compiledContextTrace(applied, node, sources, digest)
	var existing []byte
	if err := pfdb.QueryRow(ctx, db, `SELECT compiled_context_json FROM workflow_graph_node_runs WHERE id = $1`, nodeRunID).Scan(&existing); err != nil && !errors.Is(err, sqldb.ErrNoRows) {
		return err
	}
	merged := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &merged)
	}
	if title, ok := merged["node_title"].(string); ok && strings.TrimSpace(title) != "" {
		trace["node_title"] = title
	}
	compiled, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	_, err = pfdb.Exec(ctx, db, `UPDATE workflow_graph_node_runs SET compiled_context_json = $2 WHERE id = $1`, nodeRunID, compiled)
	return err
}

func validateGeneratedPayload(artifactType string, payload map[string]any) error {
	if payload == nil {
		return fmt.Errorf("提示词 provider 未返回结构化输出")
	}
	switch artifactType {
	case "creative_brief":
		fields, _ := nodeConfigFields(NodeCreativeBrief)
		return validateConfigFields(fields, payload, "")
	case "visual_system":
		return validateConfigFields(visualOverlayFields(), payload, "visual_overlay")
	case "prompt":
		return validateConfigFields(promptFields(), payload, "prompt")
	default:
		return nil
	}
}
