package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

func (e Executor) executeClaimedNode(ctx context.Context, runID, nodeRunID string) error {
	err := e.runClaimedNode(ctx, runID, nodeRunID)
	if err == nil || isProviderUnknown(err) {
		return err
	}
	var app apperr.Error
	reason := "节点运行失败"
	if errors.As(err, &app) {
		reason = app.Detail
	}
	_ = failClaimedNode(ctx, e.Pool, runID, nodeRunID, reason)
	return nil
}

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
	if nodeRun.NodeID == nil {
		return apperr.Validation("运行节点已从当前图中删除")
	}
	applied, err := appliedGraphFromSnapshot(run.Snapshot)
	if err != nil {
		return err
	}
	sources := sourcesFromSnapshot(run.Snapshot)
	sources = hydrateSourcesFromNodeRuns(ctx, e.Pool, run.NodeRuns, sources)
	node, err := applied.Node(*nodeRun.NodeID)
	if err != nil {
		return err
	}
	digest, err := compileInputDigest(applied, node.ID, sources)
	if err != nil {
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
	req := PromptRequest{
		NodeType:    node.NodeType,
		NodeTitle:   node.Title,
		InputDigest: digest,
		Config:      cloneMap(node.Config),
		Facts:       sources[node.ID].Facts,
	}
	switch node.NodeType {
	case NodeCreativeBrief:
		result, promote, err := e.callProvider(ctx, run.ID, *nodeRun, prompt.Name(), digest, node.NodeType, func() (PromptResult, error) {
			return prompt.GenerateCreativeBrief(ctx, req)
		})
		if err != nil || result.Payload == nil {
			return err
		}
		return e.persistContentArtifact(ctx, run, *nodeRun, "creative_brief", result, digest, promote, func(config map[string]any) map[string]any {
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
		if err != nil || result.Payload == nil {
			return err
		}
		return e.persistContentArtifact(ctx, run, *nodeRun, "visual_system", result, digest, promote, func(config map[string]any) map[string]any {
			out := cloneMap(config)
			out["visual_overlay"] = result.Payload
			return out
		})
	case NodePromptGeneration:
		result, promote, err := e.callProvider(ctx, run.ID, *nodeRun, prompt.Name(), digest, node.NodeType, func() (PromptResult, error) {
			return prompt.GeneratePrompt(ctx, req)
		})
		if err != nil || result.Payload == nil {
			return err
		}
		return e.persistContentArtifact(ctx, run, *nodeRun, "prompt", result, digest, promote, func(config map[string]any) map[string]any {
			out := cloneMap(config)
			out["prompt"] = result.Payload
			return out
		})
	case NodeImageGeneration:
		imgReq := ImageRequest{NodeTitle: node.Title, InputDigest: digest, GenerationSpec: map[string]any{}}
		if spec, ok := node.Config["generation_spec"].(map[string]any); ok {
			imgReq.GenerationSpec = spec
		}
		if promptPayload, ok := node.Config["prompt"].(map[string]any); ok {
			imgReq.Prompt = promptPayload
		}
		img, promote, err := e.callImageProvider(ctx, run.ID, *nodeRun, image.Name(), digest, node.NodeType, func() (ImageResult, error) {
			return image.GenerateImage(ctx, imgReq)
		})
		if err != nil || len(img.Bytes) == 0 {
			return err
		}
		return e.persistImageArtifact(ctx, run, *nodeRun, node, img, digest, promote)
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
	output, _ := json.Marshal(map[string]any{"artifact_id": *record.CurrentArtifactID, "skipped": true})
	return true, tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		_, err := pgxTx.Exec(ctx, `
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
		_ = e.markUnknownCommitted(ctx, runID, nodeRun.ID, &attemptID)
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
		_ = e.markUnknownCommitted(ctx, runID, nodeRun.ID, &attemptID)
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
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		ok, err := advanceNodePhase(ctx, pgxTx, nodeRunID, attemptID, "prepared")
		if err != nil || !ok {
			return err
		}
		ok, err = ensureProviderEffectIntent(ctx, pgxTx, nodeRunID, attemptID, hash, providerName, raw)
		if err != nil || !ok {
			return err
		}
		ok, err = advanceNodePhase(ctx, pgxTx, nodeRunID, attemptID, "provider_call")
		if err != nil || !ok {
			return err
		}
		return nil
	})
}

func (e Executor) finishProviderCall(ctx context.Context, runID, nodeRunID, attemptID string, resultJSON map[string]any) (bool, error) {
	var promote bool
	err := tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		ok, err := recordProviderEffectResult(ctx, pgxTx, nodeRunID, attemptID, resultJSON)
		if err != nil || !ok {
			return err
		}
		ok, err = advanceNodePhase(ctx, pgxTx, nodeRunID, attemptID, "provider_result_received")
		if err != nil || !ok {
			return err
		}
		var runStatus, nodeStatus string
		var active *string
		if err := pgxTx.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&runStatus); err != nil {
			return err
		}
		if err := pgxTx.QueryRow(ctx, `SELECT status, active_attempt_id FROM workflow_graph_node_runs WHERE id = $1 FOR UPDATE`, nodeRunID).Scan(&nodeStatus, &active); err != nil {
			return err
		}
		promote = runStatus == RunStatusRunning && nodeStatus == NodeRunRunning && active != nil && *active == attemptID
		return nil
	})
	return promote, err
}

func (e Executor) markUnknownCommitted(ctx context.Context, runID, nodeRunID string, attemptID *string) error {
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
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
	writeback func(map[string]any) map[string]any,
) error {
	payload, err := json.Marshal(result.Payload)
	if err != nil {
		return err
	}
	hash := sha256Hex(payload)
	now := time.Now().UTC()
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
		artifactID, err := upsertArtifact(ctx, pgxTx, run, nodeRun, artifactType, payload, hash, digest, result.Model, nil)
		if err != nil {
			return err
		}
		if promote && nodeRun.NodeID != nil {
			var liveRevision int
			if err := pgxTx.QueryRow(ctx, `SELECT revision FROM workflow_graphs WHERE id = $1`, run.GraphID).Scan(&liveRevision); err != nil {
				return err
			}
			if liveRevision == run.GraphRevision {
				if _, err := pgxTx.Exec(ctx, `UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2`, artifactID, *nodeRun.NodeID); err != nil {
					return err
				}
				if writeback != nil {
					var configJSON []byte
					if err := pgxTx.QueryRow(ctx, `SELECT config_json FROM workflow_graph_nodes WHERE id = $1`, *nodeRun.NodeID).Scan(&configJSON); err != nil {
						return err
					}
					config := map[string]any{}
					_ = json.Unmarshal(configJSON, &config)
					updated, err := json.Marshal(writeback(config))
					if err != nil {
						return err
					}
					if _, err := pgxTx.Exec(ctx, `UPDATE workflow_graph_nodes SET config_json = $2 WHERE id = $1`, *nodeRun.NodeID, updated); err != nil {
						return err
					}
				}
			}
		}
		output, _ := json.Marshal(map[string]any{"artifact_id": artifactID})
		_, err = pgxTx.Exec(ctx, `
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
) error {
	if e.Deps.Assets == nil {
		return apperr.Validation("节点运行失败")
	}
	var productID string
	if err := tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
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
	return tx.With(ctx, e.Pool, func(pgxTx pgx.Tx) error {
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
		payloadMap := map[string]any{
			"schema_version":         3,
			"product_image_asset_id": assetID,
			"generation_spec":        node.Config["generation_spec"],
			"measured_output": map[string]any{
				"mime_type":       img.MIME,
				"provider_status": img.ProviderStatus,
			},
		}
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return err
		}
		hash := sha256Hex(payload)
		artifactID, err := upsertArtifact(ctx, pgxTx, run, nodeRun, "image", payload, hash, digest, img.Model, &assetID)
		if err != nil {
			return err
		}
		if promote && nodeRun.NodeID != nil {
			var liveRevision int
			if err := pgxTx.QueryRow(ctx, `SELECT revision FROM workflow_graphs WHERE id = $1`, run.GraphID).Scan(&liveRevision); err != nil {
				return err
			}
			if liveRevision == run.GraphRevision {
				if _, err := pgxTx.Exec(ctx, `UPDATE workflow_graph_nodes SET current_artifact_id = $1 WHERE id = $2`, artifactID, *nodeRun.NodeID); err != nil {
					return err
				}
			}
			if e.Deps.Delivery != nil {
				_ = e.Deps.Delivery.QueueAfterImageSuccess(ctx, pgxTx, *nodeRun.NodeID, assetID)
			}
		}
		output, _ := json.Marshal(map[string]any{"artifact_id": artifactID})
		if _, err := pgxTx.Exec(ctx, `
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

func upsertArtifact(
	ctx context.Context,
	tx pgx.Tx,
	run graphRunRow,
	nodeRun graphNodeRunRow,
	artifactType string,
	payload []byte,
	payloadHash, digest, model string,
	assetID *string,
) (string, error) {
	var existing string
	err := tx.QueryRow(ctx, `SELECT id FROM workflow_graph_artifacts WHERE node_run_id = $1`, nodeRun.ID).Scan(&existing)
	if err == nil {
		_, err = tx.Exec(ctx, `
			UPDATE workflow_graph_artifacts SET
				artifact_type = $2, schema_version = 3, graph_revision = $3,
				payload_json = $4, payload_hash = $5, input_digest = $6,
				product_image_asset_id = $7, provider_model = $8
			WHERE id = $1
		`, existing, artifactType, run.GraphRevision, payload, payloadHash, digest, assetID, model)
		return existing, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	id := clockid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_graph_artifacts (
			id, graph_id, node_id, node_run_id, artifact_type, schema_version, graph_revision,
			payload_json, payload_hash, input_digest, product_image_asset_id, provider_name, provider_model, created_at
		) VALUES ($1, $2, $3, $4, $5, 3, $6, $7, $8, $9, $10, 'mock', $11, NOW())
	`, id, run.GraphID, nodeRun.NodeID, nodeRun.ID, artifactType, run.GraphRevision, payload, payloadHash, digest, assetID, model)
	return id, err
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

func hydrateSourcesFromNodeRuns(ctx context.Context, pool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, nodeRuns []graphNodeRunRow, sources map[string]SourceRecord) map[string]SourceRecord {
	var ids []string
	for _, item := range nodeRuns {
		if item.Status == NodeRunSucceeded {
			ids = append(ids, item.ID)
		}
	}
	if len(ids) == 0 {
		return sources
	}
	rows, err := pool.Query(ctx, `
		SELECT node_run_id, id, artifact_type, payload_json, input_digest, product_image_asset_id
		FROM workflow_graph_artifacts
		WHERE node_run_id = ANY($1)
	`, ids)
	if err != nil {
		return sources
	}
	defer rows.Close()
	byNodeRun := map[string]SourceRecord{}
	for rows.Next() {
		var nodeRunID, artifactID, artifactType, digest string
		var payload []byte
		var assetID *string
		if err := rows.Scan(&nodeRunID, &artifactID, &artifactType, &payload, &digest, &assetID); err != nil {
			return sources
		}
		payloadMap := map[string]any{}
		_ = json.Unmarshal(payload, &payloadMap)
		rec := SourceRecord{
			CurrentArtifactID:      &artifactID,
			CurrentArtifactType:    &artifactType,
			CurrentArtifactPayload: payloadMap,
			CurrentInputDigest:     &digest,
			CurrentOutputAssetID:   assetID,
		}
		byNodeRun[nodeRunID] = rec
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
		sources[*item.NodeID] = existing
	}
	return sources
}
