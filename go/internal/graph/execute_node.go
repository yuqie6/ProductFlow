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

	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func (e Executor) executeClaimedNode(ctx context.Context, runID, nodeRunID, attemptID string) error {
	err := e.runClaimedNode(ctx, runID, nodeRunID, attemptID)
	if err == nil || isProviderUnknown(err) || errors.Is(err, errProviderFenced) {
		return err
	}
	var app apperr.Error
	reason := err.Error()
	if errors.As(err, &app) {
		reason = app.Detail
	}
	if reason == "" {
		reason = "节点运行失败"
	}
	if failErr := failClaimedNode(ctx, e.DB, runID, nodeRunID, attemptID, reason); failErr != nil {
		return failErr
	}
	return nil
}

var errProviderFenced = errors.New("graph provider call fenced")

func (e Executor) runClaimedNode(ctx context.Context, runID, nodeRunID, expectedAttemptID string) error {
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
	if expectedAttemptID == "" || nodeRun.ActiveAttemptID == nil || *nodeRun.ActiveAttemptID != expectedAttemptID {
		return errProviderFenced
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
	forceTarget := isForceTarget(run, node.ID)
	mode := validDocumentAction(run.DocumentAction)
	if isContentNodeType(node.NodeType) && !contentNodeShouldGenerate(node, forceTarget, mode) {
		return e.skipFrozenContent(ctx, run, *nodeRun, sources)
	}
	if skipped, err := e.skipUnchanged(ctx, run, *nodeRun, sources, digest); err != nil || skipped {
		return err
	}
	productID, err := loadProductIDForGraph(ctx, e.DB, run.GraphID)
	if err != nil {
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
	loadedRefs, err := e.loadReferences(ctx, productID, promptRefs)
	if err != nil {
		return err
	}
	req, err := AssemblePromptRequest(node, facts, briefs, visual, loadedRefs, digest, applied)
	if err != nil {
		return err
	}
	req.DocumentAction = mode
	if req.DocumentAction == "" {
		req.DocumentAction = DocumentActionComplete
	}
	req.CurrentDocument = visibleDocument(node.NodeType, node.Config)
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
			return mergeGeneratedBrief(config, result.Payload, mode, DocumentOrigin(node))
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
			return mergeGeneratedOverlay(config, result.Payload, mode, DocumentOrigin(node))
		})
	case NodeImagePrompt:
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
			return mergeGeneratedPrompt(config, result.Payload, mode, DocumentOrigin(node))
		})
	case NodeImageGeneration:
		imgReq := ImageRequest{NodeTitle: node.Title, InputDigest: digest, GenerationSpec: map[string]any{}}
		if spec, ok := node.Config["generation_spec"].(map[string]any); ok {
			imgReq.GenerationSpec = spec
		}
		if key, ok := node.Config["image_type_key"].(string); ok {
			imgReq.ImageTypeKey = key
		}
		promptPayload, artifactID, err := incomingPromptDocument(applied, node.ID, sources)
		if err != nil {
			return err
		}
		imgReq.Prompt = promptPayload
		imgReq.PromptArtifactID = artifactID
		imgReq.References = loadedRefs
		imgReq.VisualSystem = mergeImageVisual(visual, visualOverlayFromConfig(node.Config))
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

func isForceTarget(run graphRunRow, nodeID string) bool {
	if !run.Force || nodeID == "" {
		return false
	}
	if ptrStr(run.RequestedNodeID) == nodeID {
		return true
	}
	for _, id := range run.RequestedNodeIDs {
		if id == nodeID {
			return true
		}
	}
	return false
}

func (e Executor) skipUnchanged(ctx context.Context, run graphRunRow, nodeRun graphNodeRunRow, sources map[string]SourceRecord, digest string) (bool, error) {
	if nodeRun.NodeID == nil {
		return false, nil
	}
	if isForceTarget(run, *nodeRun.NodeID) {
		return false, nil
	}
	record := sources[*nodeRun.NodeID]
	if record.CurrentArtifactID == nil || record.CurrentInputDigest == nil || *record.CurrentInputDigest != digest {
		return false, nil
	}
	return true, e.markNodeSkipped(ctx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID, record)
}

func (e Executor) skipFrozenContent(ctx context.Context, run graphRunRow, nodeRun graphNodeRunRow, sources map[string]SourceRecord) error {
	record := SourceRecord{}
	if nodeRun.NodeID != nil {
		record = sources[*nodeRun.NodeID]
	}
	return e.markNodeSkipped(ctx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID, record)
}

func (e Executor) markNodeSkipped(ctx context.Context, runID, nodeRunID string, attemptID *string, record SourceRecord) error {
	now := time.Now().UTC()
	skippedOut := map[string]any{"skipped": true}
	if record.CurrentArtifactID != nil {
		skippedOut["artifact_id"] = *record.CurrentArtifactID
	}
	if record.CurrentOutputAssetID != nil && *record.CurrentOutputAssetID != "" {
		skippedOut["product_image_asset_id"] = *record.CurrentOutputAssetID
	}
	output, _ := json.Marshal(skippedOut)
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		if attemptID == nil || *attemptID == "" {
			return apperr.Validation("节点运行缺少 attempt token")
		}
		promotable, err := lockNodeRunForPromotion(ctx, pgxTx, runID, nodeRunID, attemptID)
		if err != nil {
			return err
		}
		if !promotable {
			return nil
		}
		outputStr := string(output)
		updates := terminalNodeRunUpdates(NodeRunSkipped, now)
		updates["output_json"] = outputStr
		res := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
			Where("id = ? AND status = ? AND active_attempt_id = ?", nodeRunID, NodeRunRunning, *attemptID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		if err := appendGraphRunEvent(ctx, pgxTx, runID, "node.skipped", &nodeRunID, map[string]any{
			"status": NodeRunSkipped, "output": skippedOut,
		}); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
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
	if err := e.prepareProviderCall(ctx, runID, nodeRun.ID, attemptID, providerName, request); err != nil {
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
	if err := e.prepareProviderCall(ctx, runID, nodeRun.ID, attemptID, providerName, request); err != nil {
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

func (e Executor) prepareProviderCall(ctx context.Context, runID, nodeRunID, attemptID, providerName string, request map[string]any) error {
	hash, err := providerEffectHash(request)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(request)
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		ok, err := advanceNodePhase(ctx, pgxTx, runID, nodeRunID, attemptID, "prepared")
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
		ok, err = advanceNodePhase(ctx, pgxTx, runID, nodeRunID, attemptID, "provider_call")
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
		// Keep the same run -> node -> effect order used by cancellation and
		// unknown-result fencing before touching the provider effect row.
		var run schema.WorkflowGraphRuns
		if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&run).Error; err != nil {
			return err
		}
		if run.Status != RunStatusRunning {
			promote = false
			return nil
		}
		ok, err := recordProviderEffectResult(ctx, pgxTx, nodeRunID, attemptID, resultJSON)
		if err != nil {
			return err
		}
		if !ok {
			promote = false
			return nil
		}
		ok, err = advanceNodePhase(ctx, pgxTx, runID, nodeRunID, attemptID, "provider_result_received")
		if err != nil {
			return err
		}
		if !ok {
			promote = false
			return nil
		}
		var node schema.WorkflowGraphNodeRuns
		if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", nodeRunID).Take(&node).Error; err != nil {
			return err
		}
		promote = run.Status == RunStatusRunning && node.Status == NodeRunRunning && node.ActiveAttemptID != nil && *node.ActiveAttemptID == attemptID
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
	snapshotGraph, err := appliedGraphFromSnapshot(run.Snapshot)
	if err != nil {
		return err
	}
	if nodeRun.NodeID == nil {
		return apperr.Validation("文稿节点运行缺少 node_id")
	}
	snapshotNode, err := snapshotGraph.Node(*nodeRun.NodeID)
	if err != nil {
		return err
	}
	action := validDocumentAction(run.DocumentAction)
	if action == "" {
		action = DocumentActionComplete
	}
	baseDocumentHash := documentBaseHash(snapshotNode)
	autoPublish := run.DocumentAction == "" && DocumentOrigin(snapshotNode) == OriginSeed
	payload, err := json.Marshal(result.Payload)
	if err != nil {
		return err
	}
	hash := sha256Hex(payload)
	now := time.Now().UTC()
	adoptionAttempted := promote && autoPublish && writeback != nil
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		var productID string
		if adoptionAttempted {
			id, err := loadProductIDForGraph(ctx, pgxTx, run.GraphID)
			if err != nil {
				return err
			}
			productID = id
			// 采用走 Mutate 会改写整图节点行。先锁 graph，避免与并行内容节点的 artifact/节点行锁形成环。
			if _, err := loadGraphForUpdate(ctx, pgxTx, productID, run.GraphID); err != nil {
				return err
			}
		}
		artifactID, err := upsertArtifact(ctx, pgxTx, run, nodeRun, artifactType, payload, hash, digest, providerName, result.Model, nil)
		if err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphArtifacts{}).Where("id = ?", artifactID).
			Updates(map[string]any{"document_action": action, "base_document_hash": baseDocumentHash}).Error; err != nil {
			return err
		}
		if !promote {
			return finishUnpromotedNodeRun(ctx, pgxTx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID, now)
		}
		promotable, err := lockNodeRunForPromotion(ctx, pgxTx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID)
		if err != nil {
			return err
		}
		if !promotable {
			return finishUnpromotedNodeRun(ctx, pgxTx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID, now)
		}
		adopted := false
		if promote {
			if adoptionAttempted {
				revision, didAdopt, err := adoptGeneratedDocument(ctx, pgxTx, productID, run.GraphID, *nodeRun.NodeID, adoptSummary(artifactType), run.Snapshot, writeback)
				if err != nil {
					return err
				}
				adopted = didAdopt
				if didAdopt {
					if err := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphRuns{}).Where("id = ?", run.ID).
						Select("graph_revision").
						Updates(map[string]any{"graph_revision": revision}).Error; err != nil {
						return err
					}
				}
			}
			candidateID := any(artifactID)
			if adopted {
				candidateID = nil
			}
			if err := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).Where("id = ?", *nodeRun.NodeID).
				Select("current_artifact_id", "pending_candidate_artifact_id").
				Updates(map[string]any{"current_artifact_id": nil, "pending_candidate_artifact_id": candidateID}).Error; err != nil {
				return err
			}
		}
		outputPayload := map[string]any{"artifact_id": artifactID, "disposition": "candidate"}
		if adoptionAttempted {
			outputPayload["adopted"] = adopted
			if adopted {
				outputPayload["disposition"] = "published"
			}
		}
		output, _ := json.Marshal(outputPayload)
		outputStr := string(output)
		updates := terminalNodeRunUpdates(NodeRunSucceeded, now)
		updates["output_json"] = outputStr
		result := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ? AND status = ?", nodeRun.ID, NodeRunRunning).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID)
			return err
		}
		if err := appendGraphRunEvent(ctx, pgxTx, run.ID, "node.succeeded", &nodeRun.ID, map[string]any{
			"status": NodeRunSucceeded, "node_id": nodeRun.NodeID, "output": outputPayload,
		}); err != nil {
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
			return finishUnpromotedNodeRun(ctx, pgxTx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID, now)
		}
		promotable, err := lockNodeRunForPromotion(ctx, pgxTx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID)
		if err != nil {
			return err
		}
		if !promotable {
			return finishUnpromotedNodeRun(ctx, pgxTx, run.ID, nodeRun.ID, nodeRun.ActiveAttemptID, now)
		}
		if nodeRun.NodeID != nil {
			var live schema.WorkflowGraphs
			if err := pgxTx.WithContext(ctx).Select("revision").Where("id = ?", run.GraphID).Take(&live).Error; err != nil {
				return err
			}
			if live.Revision == run.GraphRevision {
				if err := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodes{}).Where("id = ?", *nodeRun.NodeID).
					Select("current_artifact_id").
					Updates(map[string]any{"current_artifact_id": artifactID}).Error; err != nil {
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
		outputStr := string(output)
		updates := terminalNodeRunUpdates(NodeRunSucceeded, now)
		updates["output_json"] = outputStr
		result := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ? AND status = ?", nodeRun.ID, NodeRunRunning).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID)
			return err
		}
		if err := appendGraphRunEvent(ctx, pgxTx, run.ID, "node.succeeded", &nodeRun.ID, map[string]any{
			"status": NodeRunSucceeded, "node_id": nodeRun.NodeID, "output": map[string]any{
				"artifact_id": artifactID, "product_image_asset_id": assetID,
			},
		}); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, run.ID)
		return err
	})
}

func (e Executor) loadReferences(ctx context.Context, productID string, refs []compiledReference) ([]ReferenceImage, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if e.Deps.Assets == nil {
		return nil, apperr.Validation("参考图不属于该商品")
	}
	out := make([]ReferenceImage, 0, len(refs))
	for _, ref := range refs {
		data, mime, filename, err := e.Deps.Assets.ReadAssetBytes(ctx, e.DB, productID, ref.AssetID)
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
	var existing schema.WorkflowGraphArtifacts
	err := tx.WithContext(ctx).Select("id").Where("node_run_id = ?", nodeRun.ID).Take(&existing).Error
	if err == nil {
		err = tx.WithContext(ctx).Model(&schema.WorkflowGraphArtifacts{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"artifact_type":          artifactType,
			"schema_version":         3,
			"graph_revision":         run.GraphRevision,
			"payload_json":           string(payload),
			"payload_hash":           payloadHash,
			"input_digest":           digest,
			"product_image_asset_id": assetID,
			"provider_name":          providerName,
			"provider_model":         model,
		}).Error
		return existing.ID, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	id := clockid.New()
	err = tx.WithContext(ctx).Create(&schema.WorkflowGraphArtifacts{
		ID:                  id,
		GraphID:             run.GraphID,
		NodeID:              nodeRun.NodeID,
		NodeRunID:           &nodeRun.ID,
		ArtifactType:        artifactType,
		SchemaVersion:       3,
		GraphRevision:       run.GraphRevision,
		PayloadJSON:         string(payload),
		PayloadHash:         payloadHash,
		InputDigest:         digest,
		ProductImageAssetID: assetID,
		ProviderName:        &providerName,
		ProviderModel:       &model,
		CreatedAt:           time.Now().UTC(),
	}).Error
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
	var recs []schema.WorkflowGraphArtifacts
	if err := pool.WithContext(ctx).
		Where("node_run_id IN ?", ids).
		Find(&recs).Error; err != nil {
		return nil, err
	}
	byNodeRun := map[string]SourceRecord{}
	for _, rec := range recs {
		if rec.NodeRunID == nil {
			continue
		}
		payloadMap := map[string]any{}
		if rec.PayloadJSON != "" {
			if err := json.Unmarshal([]byte(rec.PayloadJSON), &payloadMap); err != nil {
				return nil, err
			}
		}
		digest := rec.InputDigest
		artifactID := rec.ID
		artifactType := rec.ArtifactType
		source := SourceRecord{
			CurrentArtifactID:      &artifactID,
			CurrentArtifactType:    &artifactType,
			CurrentArtifactPayload: payloadMap,
			CurrentInputDigest:     &digest,
			CurrentOutputAssetID:   rec.ProductImageAssetID,
		}
		switch artifactType {
		case "creative_brief":
			source.Brief = cloneMap(payloadMap)
		case "visual_system":
			if overlay, ok := payloadMap["visual_overlay"].(map[string]any); ok {
				source.VisualPayload = cloneMap(overlay)
			} else {
				source.VisualPayload = cloneMap(payloadMap)
			}
		case "prompt":
			source.PromptDocument = cloneMap(payloadMap)
		}
		byNodeRun[*rec.NodeRunID] = source
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
		if rec.PromptDocument != nil {
			existing.PromptDocument = rec.PromptDocument
		}
		sources[*item.NodeID] = existing
	}
	return sources, nil
}

func writeCompiledContext(ctx context.Context, db *gorm.DB, nodeRunID string, node AppliedNode, applied AppliedGraph, sources map[string]SourceRecord, digest string) error {
	trace := compiledContextTrace(applied, node, sources, digest)
	var rec schema.WorkflowGraphNodeRuns
	if err := db.WithContext(ctx).Select("compiled_context_json").Where("id = ?", nodeRunID).Take(&rec).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	existing := jsonPtrBytes(rec.CompiledContextJSON)
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
	compiledStr := string(compiled)
	return db.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).Where("id = ?", nodeRunID).Updates(map[string]any{
		"compiled_context_json": compiledStr,
	}).Error
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

func finishUnpromotedNodeRun(ctx context.Context, pgxTx *gorm.DB, runID, nodeRunID string, attemptID *string, now time.Time) error {
	reason := GraphCancelledReason
	if attemptID == nil || *attemptID == "" {
		_, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
		return err
	}
	promotable, err := lockNodeRunForPromotion(ctx, pgxTx, runID, nodeRunID, attemptID)
	if err != nil {
		return err
	}
	if !promotable {
		return nil
	}
	updates := terminalNodeRunUpdates(NodeRunCancelled, now)
	updates["failure_reason"] = reason
	res := pgxTx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
		Where("id = ? AND status IN ? AND active_attempt_id = ?", nodeRunID, []string{NodeRunQueued, NodeRunRunning}, *attemptID).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return nil
	}
	if err := appendGraphRunEvent(ctx, pgxTx, runID, "node.cancelled", &nodeRunID, map[string]any{
		"status": NodeRunCancelled, "reason": reason,
	}); err != nil {
		return err
	}
	_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
	return err
}

// lockNodeRunForPromotion closes the gap between provider result fencing and
// artifact promotion. Cancellation or a newer attempt wins before any live
// node/config projection is changed.
func lockNodeRunForPromotion(ctx context.Context, pgxTx *gorm.DB, runID, nodeRunID string, attemptID *string) (bool, error) {
	var run schema.WorkflowGraphRuns
	if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", runID).Take(&run).Error; err != nil {
		return false, err
	}
	if run.Status != RunStatusRunning {
		return false, nil
	}
	var node schema.WorkflowGraphNodeRuns
	if err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ? AND graph_run_id = ?", nodeRunID, runID).Take(&node).Error; err != nil {
		return false, err
	}
	if node.Status != NodeRunRunning || attemptID == nil || node.ActiveAttemptID == nil {
		return false, nil
	}
	return *node.ActiveAttemptID == *attemptID, nil
}

func adoptSummary(artifactType string) string {
	switch artifactType {
	case "creative_brief":
		return "采用生成的创作要求"
	case "visual_system":
		return "采用生成的视觉规范"
	case "prompt":
		return "采用生成的提示词"
	default:
		return "采用生成文稿"
	}
}

func adoptGeneratedDocument(
	ctx context.Context,
	pgxTx *gorm.DB,
	productID, graphID, nodeID, summary string,
	snapshot map[string]any,
	writeback func(map[string]any) map[string]any,
) (int, bool, error) {
	row, err := loadGraphForUpdate(ctx, pgxTx, productID, graphID)
	if err != nil {
		return 0, false, err
	}
	applied, err := loadAppliedGraph(ctx, pgxTx, row)
	if err != nil {
		return 0, false, err
	}
	node, err := applied.Node(nodeID)
	if err != nil {
		return 0, false, err
	}
	if snapshot == nil {
		return row.Revision, false, nil
	}
	snapshotGraph, err := appliedGraphFromSnapshot(snapshot)
	if err != nil {
		return 0, false, err
	}
	snapshotNode, snapErr := snapshotGraph.Node(nodeID)
	if snapErr != nil || liveDocumentDivergedFromSnapshot(node, snapshotNode) {
		return row.Revision, false, nil
	}
	merged := writeback(originConfigForInvert(node))
	result, err := Mutate(ctx, pgxTx, productID, graphID, ChangeSet{
		BaseGraphRevision: row.Revision,
		Summary:           summary,
		ActorType:         ActorSystem,
		Operations: []Operation{UpdateNodeConfigOp{
			NodeRef:        nodeID,
			Config:         merged,
			DocumentOrigin: strPtr(OriginGenerated),
		}},
	}, HistoryEdit)
	if err == nil {
		return result.Revision, true, nil
	}
	var app apperr.Error
	if errors.As(err, &app) && app.Status == 409 {
		return row.Revision, false, nil
	}
	return 0, false, err
}
