package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// evalWriteObservation keeps normal business mismatches separate from failures
// in the observer itself. The Node runner turns only ReadbackErrors into an
// unobservable terminal; Errors remain ordinary task failures.
type evalWriteObservation struct {
	Errors         []string
	ReadbackErrors []string
}

func (o *evalWriteObservation) business(detail string) {
	o.Errors = append(o.Errors, detail)
}

func (o *evalWriteObservation) readback(detail string) {
	o.ReadbackErrors = append(o.ReadbackErrors, detail)
}

func (o evalWriteObservation) allErrors() []string {
	return append(append([]string{}, o.Errors...), o.ReadbackErrors...)
}

func gradeEvalPersistedWrites(t *testing.T, as *agentServer, seeded seededEvalWorld, expectations []EvalWriteExpect) []string {
	t.Helper()
	return observeEvalPersistedWrites(t, as, seeded, expectations).allErrors()
}

func observeEvalPersistedWrites(t *testing.T, as *agentServer, seeded seededEvalWorld, expectations []EvalWriteExpect) evalWriteObservation {
	t.Helper()
	var observation evalWriteObservation
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	for _, expected := range expectations {
		var params any
		var queryErr error
		switch expected.Tool {
		case "apply_graph_change_set_v1":
			live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
			if err != nil {
				observation.readback("graph: " + err.Error())
				continue
			}
			var business, readback []string
			params, business, readback = observeEvalGraphApply(t, seeded.InitialGraph, live, expected.Match, seeded)
			for _, detail := range business {
				observation.business(detail)
			}
			for _, detail := range readback {
				observation.readback(detail)
			}
		case "propose_graph_change_set_v1":
			var raw []byte
			queryErr = as.pool.QueryRow(ctx, `SELECT change_set_json FROM workflow_graph_proposals WHERE graph_id=$1 AND status='pending' ORDER BY created_at DESC LIMIT 1`, seeded.GraphID).Scan(&raw)
			if queryErr == nil {
				if err := json.Unmarshal(raw, &params); err != nil {
					observation.readback("graph proposal JSON: " + err.Error())
					continue
				}
				live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
				if err != nil {
					observation.readback("graph: " + err.Error())
					continue
				}
				if live.PendingProposal == nil {
					observation.business("pending graph proposal is not projected")
				}
				if !evalGraphContentEqual(seeded.InitialGraph, live) {
					observation.business("pending graph proposal changed live graph")
				}
			} else if errors.Is(queryErr, pgx.ErrNoRows) {
				observation.business("pending graph proposal was not persisted")
				continue
			} else {
				observation.readback("graph proposal: " + queryErr.Error())
				continue
			}
		case "propose_global_draft":
			draft, err := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
			if err == nil && draft.CurrentRevision != nil {
				var payload any
				if err := json.Unmarshal(draft.CurrentRevision.Payload, &payload); err != nil {
					observation.readback("library draft JSON: " + err.Error())
					continue
				}
				params = map[string]any{"draft_kind": "library_organization", "library_payload": payload}
				if draft.Status != "awaiting_confirmation" && draft.Status != "confirmed" {
					observation.business("library draft has unexpected status: " + draft.Status)
				}
				if draft.Status == "confirmed" {
					observeConfirmedEvalLibraryOperations(t, as, seeded, payload, &observation)
				}
			} else if err != nil && !apperr.IsNotFound(err) {
				observation.readback("library draft: " + err.Error())
			} else {
				observation.business("library draft was not persisted")
			}
		case "request_workflow_run_v1", "request_global_workflow_run_v1":
			var productID *string
			if expected.Tool == "request_workflow_run_v1" {
				productID = &seeded.ProductID
			}
			request, err := as.svc.GetWorkflowRunRequest(ctx, productID, seeded.ConvID, nil)
			if err == nil && request != nil {
				action := ""
				if request.DocumentAction != nil {
					action = *request.DocumentAction
				}
				params = map[string]any{"scope": request.RunScope, "node_id": request.TargetNodeID, "node_ids": request.TargetNodeIDs,
					"product_id": request.ProductID, "workflow_id": request.WorkflowID, "source_run_id": request.SourceRunID,
					"force": request.Force, "document_action": action}
			} else if err != nil {
				observation.readback("workflow run request: " + err.Error())
			} else {
				observation.business("workflow run request was not persisted")
			}
		case "finalize_product_intake_v1":
			var raw []byte
			if err := as.pool.QueryRow(ctx, `SELECT intake_json FROM products WHERE id=$1`, seeded.ProductID).Scan(&raw); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					observation.business("product intake was not persisted")
				} else {
					observation.readback("product intake: " + err.Error())
				}
				continue
			}
			raw = bytes.TrimSpace(raw)
			if len(raw) == 0 {
				break
			}
			var intake map[string]any
			if err := json.Unmarshal(raw, &intake); err != nil {
				observation.readback("product intake JSON: " + err.Error())
				continue
			}
			params = map[string]any{"selection": intake, "reference_asset_ids": intake["reference_asset_ids"]}
		case "create_product_workspace_v1":
			if seeded.CreatedProductID == "" {
				observation.business("created workspace identity was not observed")
				continue
			}
			var name string
			if err := as.pool.QueryRow(ctx, `SELECT name FROM products WHERE id=$1`, seeded.CreatedProductID).Scan(&name); err == nil {
				params = map[string]any{"name": name}
			} else {
				if errors.Is(err, pgx.ErrNoRows) {
					observation.business("created workspace product was not persisted")
				} else {
					observation.readback("created workspace product: " + err.Error())
				}
				continue
			}
		case "cancel_workflow_run_v1":
			if seeded.RecentRunID == "" {
				observation.business("recent run identity was not seeded")
				continue
			}
			var status string
			if err := as.pool.QueryRow(ctx, `SELECT status FROM workflow_graph_runs WHERE id=$1`, seeded.RecentRunID).Scan(&status); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					observation.business("recent run was not persisted")
				} else {
					observation.readback("recent run: " + err.Error())
				}
				continue
			}
			if status != "cancelled" {
				observation.business("recent run was not cancelled")
			}
			params = map[string]any{"run_id": seeded.RecentRunID}
		case "discard_workflow_proposal_v1":
			live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
			if err != nil {
				observation.readback("graph: " + err.Error())
				continue
			}
			if live.PendingProposal != nil {
				observation.business("discarded graph proposal remains pending")
			}
			if !evalGraphContentEqual(seeded.InitialGraph, live) {
				observation.business("discarding proposal changed live graph")
			}
			var status string
			queryErr = as.pool.QueryRow(ctx, `SELECT status FROM workflow_graph_proposals WHERE graph_id=$1 ORDER BY created_at DESC LIMIT 1`, seeded.GraphID).Scan(&status)
			if queryErr != nil {
				if errors.Is(queryErr, pgx.ErrNoRows) {
					observation.business("discarded graph proposal row was not observed")
				} else {
					observation.readback("discarded graph proposal: " + queryErr.Error())
				}
			} else if status != "discarded" {
				observation.business("graph proposal status is " + status + ", want discarded")
			}
			params = map[string]any{}
		default:
			observation.readback("observer does not support persisted write: " + expected.Tool)
			continue
		}
		params = normalizeEvalIdentity(t, params, seeded)
		if !evalMatchesPaths(params, expected.Match) {
			observation.business("persisted content mismatch: " + expected.Tool)
		}
	}
	return observation
}

func normalizeEvalIdentity(t *testing.T, value any, seeded seededEvalWorld) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	mapping := evalActualToFixtureIDs(seeded)
	for actual, fixture := range mapping {
		if actual != "" {
			text = strings.ReplaceAll(text, strconv.Quote(actual), strconv.Quote(fixture))
		}
	}
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Approval completion must be checked against resulting business content, not only draft status.
// A proposal may legitimately still be pending when the task terminal is
// awaiting_confirmation; otherwise the same observer validates the applied
// graph delta against the baseline projection.
func gradeEvalFinalWrites(t *testing.T, as *agentServer, seeded seededEvalWorld, expectations []EvalWriteExpect, before graph.Projection) []string {
	t.Helper()
	return observeEvalFinalWrites(t, as, seeded, expectations, before).allErrors()
}

func observeEvalFinalWrites(t *testing.T, as *agentServer, seeded seededEvalWorld, expectations []EvalWriteExpect, before graph.Projection) evalWriteObservation {
	t.Helper()
	var observation evalWriteObservation
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	for _, expected := range expectations {
		if expected.Tool != "propose_graph_change_set_v1" {
			if expected.Tool == "propose_global_draft" {
				ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
				draft, err := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
				if err != nil {
					if apperr.IsNotFound(err) {
						observation.business("library draft not confirmed")
					} else {
						observation.readback("library draft: " + err.Error())
					}
					continue
				}
				if draft.Status == "awaiting_confirmation" {
					continue
				}
				if draft.Status != "confirmed" {
					observation.business("library draft not confirmed")
					continue
				}
			}
			one := observeEvalPersistedWrites(t, as, seeded, []EvalWriteExpect{expected})
			observation.Errors = append(observation.Errors, one.Errors...)
			observation.ReadbackErrors = append(observation.ReadbackErrors, one.ReadbackErrors...)
			continue
		}
		after, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			observation.readback("graph: " + err.Error())
			continue
		}
		if after.PendingProposal != nil {
			one := observeEvalPersistedWrites(t, as, seeded, []EvalWriteExpect{expected})
			observation.Errors = append(observation.Errors, one.Errors...)
			observation.ReadbackErrors = append(observation.ReadbackErrors, one.ReadbackErrors...)
			continue
		}
		_, business, readback := observeEvalGraphApply(t, before, after, expected.Match, seeded)
		if len(business) > 0 {
			observation.business("final graph content mismatch")
		}
		for _, detail := range readback {
			observation.readback(detail)
		}
	}
	return observation
}

var evalObservedGraphOperations = map[string]bool{
	"create_node":         true,
	"update_node_config":  true,
	"rename_node":         true,
	"delete_node":         true,
	"connect_nodes":       true,
	"disconnect_edge":     true,
	"reorder_edges":       true,
	"move_nodes":          true,
	"create_group":        true,
	"move_nodes_to_group": true,
	"rename_group":        true,
	"dissolve_group":      true,
}

type evalExpectedGraphOperation struct {
	Index       int
	Operation   string
	Constraints map[string]any
}

func evalExpectedGraphOperations(match map[string]any) []evalExpectedGraphOperation {
	byIndex := map[int]map[string]any{}
	for path, expected := range match {
		parts := evalOperationPath.FindStringSubmatch(path)
		if parts == nil || parts[1] != "operations" {
			continue
		}
		index, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		if byIndex[index] == nil {
			byIndex[index] = map[string]any{}
		}
		setEvalNestedValue(byIndex[index], parts[3], expected)
	}
	indices := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	operations := make([]evalExpectedGraphOperation, 0, len(indices))
	for _, index := range indices {
		operation, _ := byIndex[index]["op"].(string)
		operations = append(operations, evalExpectedGraphOperation{Index: index, Operation: operation, Constraints: byIndex[index]})
	}
	return operations
}

func setEvalNestedValue(value map[string]any, path string, expected any) {
	parts := strings.Split(path, ".")
	current := value
	for _, part := range parts[:len(parts)-1] {
		child, ok := current[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			current[part] = child
		}
		current = child
	}
	if len(parts) > 0 {
		current[parts[len(parts)-1]] = expected
	}
}

// observeEvalGraphApply derives the persisted operation list from the before
// and after projections. It deliberately does not inspect the model's tool
// call. A missing or extra state change is a business failure; an operation
// outside the observer's explicit vocabulary is an observation gap.
func observeEvalGraphApply(t *testing.T, before, after graph.Projection, match map[string]any, seeded seededEvalWorld) (any, []string, []string) {
	t.Helper()
	expected := evalExpectedGraphOperations(match)
	for _, operation := range expected {
		if !evalObservedGraphOperations[operation.Operation] {
			return nil, nil, []string{"observer does not support graph operation: " + operation.Operation}
		}
	}
	if len(expected) == 0 {
		return nil, nil, []string{"observer has no graph operation contract"}
	}
	actual, err := evalGraphDelta(before, after)
	if err != nil {
		return nil, nil, []string{"graph delta: " + err.Error()}
	}
	value := normalizeEvalIdentity(t, map[string]any{"operations": actual}, seeded)
	business := []string{}
	if !evalMatchesPaths(value, match) {
		business = append(business, "persisted graph content mismatch")
	}
	if evalGraphUnexpectedInvariant(before, after, expected, seeded) {
		business = append(business, "unexpected persisted graph effect")
	}
	if len(actual) == 0 {
		business = append(business, "expected graph operation produced no persisted change")
	}
	return value, business, nil
}

func evalGraphContentEqual(before, after graph.Projection) bool {
	return before.ID == after.ID && before.ProductID == after.ProductID && before.Title == after.Title &&
		before.Revision == after.Revision && reflect.DeepEqual(before.Nodes, after.Nodes) &&
		reflect.DeepEqual(before.Edges, after.Edges) && reflect.DeepEqual(before.Groups, after.Groups)
}

func evalGraphDelta(before, after graph.Projection) ([]map[string]any, error) {
	if before.ID == "" || after.ID == "" || before.ID != after.ID || before.ProductID != after.ProductID {
		return nil, fmt.Errorf("projection identity changed")
	}
	beforeNodes := map[string]graph.NodeView{}
	afterNodes := map[string]graph.NodeView{}
	for _, node := range before.Nodes {
		beforeNodes[node.ID] = node
	}
	for _, node := range after.Nodes {
		afterNodes[node.ID] = node
	}
	beforeGroups := map[string]graph.GroupView{}
	afterGroups := map[string]graph.GroupView{}
	for _, group := range before.Groups {
		beforeGroups[group.ID] = group
	}
	for _, group := range after.Groups {
		afterGroups[group.ID] = group
	}
	beforeEdges := map[string]graph.EdgeView{}
	afterEdges := map[string]graph.EdgeView{}
	for _, edge := range before.Edges {
		beforeEdges[edge.ID] = edge
	}
	for _, edge := range after.Edges {
		afterEdges[edge.ID] = edge
	}

	operations := make([]map[string]any, 0)
	for _, group := range before.Groups {
		if _, ok := afterGroups[group.ID]; !ok {
			operations = append(operations, map[string]any{"op": "dissolve_group", "group_ref": group.ID})
		}
	}
	for _, group := range after.Groups {
		if _, ok := beforeGroups[group.ID]; !ok {
			operations = append(operations, map[string]any{"op": "create_group", "client_ref": group.ID, "title": group.Title, "member_refs": append([]string{}, group.MemberIDs...)})
			continue
		}
		if beforeGroups[group.ID].Title != group.Title {
			operations = append(operations, map[string]any{"op": "rename_group", "group_ref": group.ID, "title": group.Title})
		}
	}

	for _, node := range after.Nodes {
		if _, ok := beforeNodes[node.ID]; !ok {
			operations = append(operations, map[string]any{
				"op": "create_node", "client_ref": node.ID, "node_type": string(node.NodeType),
				"group_ref": node.GroupID, "title": node.Title, "config": node.Config,
			})
		}
	}
	for _, node := range before.Nodes {
		if _, ok := afterNodes[node.ID]; !ok {
			operations = append(operations, map[string]any{"op": "delete_node", "node_ref": node.ID})
		}
	}
	for _, node := range after.Nodes {
		old, ok := beforeNodes[node.ID]
		if !ok {
			continue
		}
		if old.Title != node.Title {
			operations = append(operations, map[string]any{"op": "rename_node", "node_ref": node.ID, "title": node.Title})
		}
		if !reflect.DeepEqual(old.Config, node.Config) || !reflect.DeepEqual(old.BoundAssetID, node.BoundAssetID) {
			operations = append(operations, map[string]any{"op": "update_node_config", "node_ref": node.ID, "config": node.Config})
		}
	}

	moveGroups := map[string][]string{}
	moveGroupOrder := []string{}
	positionMoves := [][]any{}
	dissolvedGroups := map[string]bool{}
	createdGroups := map[string]bool{}
	for _, group := range after.Groups {
		if _, ok := beforeGroups[group.ID]; !ok {
			createdGroups[group.ID] = true
		}
	}
	for _, group := range before.Groups {
		if _, ok := afterGroups[group.ID]; !ok {
			dissolvedGroups[group.ID] = true
		}
	}
	for _, node := range after.Nodes {
		old, ok := beforeNodes[node.ID]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(old.GroupID, node.GroupID) {
			if node.GroupID != nil && createdGroups[*node.GroupID] {
				// create_group.member_refs persists the initial membership in the
				// same operation; do not invent a second move operation.
			} else if old.GroupID != nil && node.GroupID == nil && dissolvedGroups[*old.GroupID] {
				// Dissolve already accounts for clearing group membership.
			} else {
				key := ""
				if node.GroupID != nil {
					key = *node.GroupID
				}
				if _, seen := moveGroups[key]; !seen {
					moveGroupOrder = append(moveGroupOrder, key)
				}
				moveGroups[key] = append(moveGroups[key], node.ID)
			}
		}
		if old.PositionX != node.PositionX || old.PositionY != node.PositionY {
			positionMoves = append(positionMoves, []any{node.ID, node.PositionX, node.PositionY})
		}
	}
	for _, groupID := range moveGroupOrder {
		var groupRef *string
		if groupID != "" {
			value := groupID
			groupRef = &value
		}
		operations = append(operations, map[string]any{"op": "move_nodes_to_group", "group_ref": groupRef, "node_refs": moveGroups[groupID]})
	}
	if len(positionMoves) > 0 {
		operations = append(operations, map[string]any{"op": "move_nodes", "nodes": positionMoves})
	}

	removedNodes := map[string]bool{}
	for _, node := range before.Nodes {
		if _, ok := afterNodes[node.ID]; !ok {
			removedNodes[node.ID] = true
		}
	}
	for _, edge := range before.Edges {
		if _, ok := afterEdges[edge.ID]; !ok {
			// delete_node already accounts for its incident-edge cascade.
			if !removedNodes[edge.SourceNodeID] && !removedNodes[edge.TargetNodeID] {
				operations = append(operations, map[string]any{"op": "disconnect_edge", "edge_ref": edge.ID})
			}
		}
	}
	for _, edge := range after.Edges {
		if _, ok := beforeEdges[edge.ID]; !ok {
			operations = append(operations, map[string]any{"op": "connect_nodes", "client_ref": edge.ID, "source_ref": edge.SourceNodeID, "target_ref": edge.TargetNodeID, "order": edge.Order})
		}
	}
	beforeOrder := evalEdgeOrders(before.Edges)
	afterOrder := evalEdgeOrders(after.Edges)
	keys := make([]evalEdgeOrderKey, 0, len(beforeOrder)+len(afterOrder))
	seenKeys := map[evalEdgeOrderKey]bool{}
	for key := range beforeOrder {
		seenKeys[key] = true
		keys = append(keys, key)
	}
	for key := range afterOrder {
		if !seenKeys[key] {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].TargetNodeID != keys[j].TargetNodeID {
			return keys[i].TargetNodeID < keys[j].TargetNodeID
		}
		return string(keys[i].Role) < string(keys[j].Role)
	})
	for _, key := range keys {
		beforeCommon := evalCommonEdgeOrder(beforeOrder[key], afterOrder[key])
		afterCommon := evalCommonEdgeOrder(afterOrder[key], beforeOrder[key])
		if len(beforeCommon) > 1 && !reflect.DeepEqual(beforeCommon, afterCommon) {
			operations = append(operations, map[string]any{"op": "reorder_edges", "node_ref": key.TargetNodeID, "role": string(key.Role), "edge_refs": afterOrder[key]})
		}
	}
	return operations, nil
}

func evalCommonEdgeOrder(order []string, other []string) []string {
	present := make(map[string]bool, len(other))
	for _, id := range other {
		present[id] = true
	}
	common := make([]string, 0, len(order))
	for _, id := range order {
		if present[id] {
			common = append(common, id)
		}
	}
	return common
}

func TestEvalGraphDeltaProjectionBoundaries(t *testing.T) {
	groupID := "group-new"
	node1Group := groupID
	base := graph.Projection{
		ID:        "graph",
		ProductID: "product",
		Title:     "workflow",
		Nodes: []graph.NodeView{
			{ID: "node-1", NodeType: graph.NodeImagePrompt, Title: "one"},
			{ID: "node-2", NodeType: graph.NodeImageGeneration, Title: "two"},
		},
		Edges: []graph.EdgeView{
			{ID: "edge-1", SourceNodeID: "node-1", TargetNodeID: "node-2", DataType: graph.DataPrompt, Role: graph.RolePrompt, Order: 0},
		},
		Groups: []graph.GroupView{},
	}

	t.Run("create group membership is one operation", func(t *testing.T) {
		after := base
		after.Groups = []graph.GroupView{{ID: groupID, Title: "new group", MemberIDs: []string{"node-1"}}}
		after.Nodes = append([]graph.NodeView(nil), base.Nodes...)
		after.Nodes[0].GroupID = &node1Group
		actual, err := evalGraphDelta(base, after)
		if err != nil {
			t.Fatal(err)
		}
		if len(actual) != 1 || actual[0]["op"] != "create_group" {
			t.Fatalf("actual delta %#v", actual)
		}
	})

	t.Run("reorder remains visible with an added edge", func(t *testing.T) {
		before := base
		before.Edges = append([]graph.EdgeView{}, base.Edges...)
		before.Edges = append(before.Edges, graph.EdgeView{ID: "edge-2", SourceNodeID: "node-1", TargetNodeID: "node-2", DataType: graph.DataPrompt, Role: graph.RolePrompt, Order: 1})
		after := before
		after.Edges = []graph.EdgeView{
			{ID: "edge-2", SourceNodeID: "node-1", TargetNodeID: "node-2", DataType: graph.DataPrompt, Role: graph.RolePrompt, Order: 0},
			{ID: "edge-1", SourceNodeID: "node-1", TargetNodeID: "node-2", DataType: graph.DataPrompt, Role: graph.RolePrompt, Order: 1},
			{ID: "edge-3", SourceNodeID: "node-1", TargetNodeID: "node-2", DataType: graph.DataPrompt, Role: graph.RolePrompt, Order: 2},
		}
		actual, err := evalGraphDelta(before, after)
		if err != nil {
			t.Fatal(err)
		}
		seenConnect, seenReorder := false, false
		for _, operation := range actual {
			if operation["op"] == "connect_nodes" {
				seenConnect = true
			}
			if operation["op"] == "reorder_edges" {
				seenReorder = true
			}
		}
		if !seenConnect || !seenReorder {
			t.Fatalf("actual delta %#v", actual)
		}
	})

	t.Run("title and existing edge identity are stable", func(t *testing.T) {
		titleChanged := base
		titleChanged.Title = "unexpected"
		if !evalGraphUnexpectedInvariant(base, titleChanged, nil, seededEvalWorld{}) {
			t.Fatal("graph title mutation was not detected")
		}
		edgeChanged := base
		edgeChanged.Edges = []graph.EdgeView{{ID: "edge-1", SourceNodeID: "node-2", TargetNodeID: "node-1", DataType: graph.DataPrompt, Role: graph.RolePrompt, Order: 0}}
		if !evalGraphUnexpectedInvariant(base, edgeChanged, nil, seededEvalWorld{}) {
			t.Fatal("existing edge endpoint mutation was not detected")
		}
		edgeRoleChanged := base
		edgeRoleChanged.Edges = []graph.EdgeView{{ID: "edge-1", SourceNodeID: "node-1", TargetNodeID: "node-2", DataType: graph.DataPrompt, Role: graph.RoleFacts, Order: 0}}
		if !evalGraphUnexpectedInvariant(base, edgeRoleChanged, nil, seededEvalWorld{}) {
			t.Fatal("existing edge role mutation was not detected")
		}
	})
}

type evalEdgeOrderKey struct {
	TargetNodeID string
	Role         graph.EdgeRole
}

func evalEdgeOrders(edges []graph.EdgeView) map[evalEdgeOrderKey][]string {
	grouped := map[evalEdgeOrderKey][]graph.EdgeView{}
	for _, edge := range edges {
		key := evalEdgeOrderKey{TargetNodeID: edge.TargetNodeID, Role: edge.Role}
		grouped[key] = append(grouped[key], edge)
	}
	orders := make(map[evalEdgeOrderKey][]string, len(grouped))
	for key, items := range grouped {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Order != items[j].Order {
				return items[i].Order < items[j].Order
			}
			return items[i].ID < items[j].ID
		})
		for _, edge := range items {
			orders[key] = append(orders[key], edge.ID)
		}
	}
	return orders
}

func evalGraphUnexpectedInvariant(before, after graph.Projection, expected []evalExpectedGraphOperation, seeded seededEvalWorld) bool {
	if before.Title != after.Title {
		return true
	}
	allowedConfig := map[string]bool{}
	for _, operation := range expected {
		if operation.Operation == "update_node_config" {
			ref, _ := operation.Constraints["node_ref"].(string)
			id := seeded.NodeIDs[ref]
			if id == "" {
				id = ref
			}
			allowedConfig[id] = true
		}
	}
	beforeNodes, afterNodes := map[string]graph.NodeView{}, map[string]graph.NodeView{}
	for _, node := range before.Nodes {
		beforeNodes[node.ID] = node
	}
	for _, node := range after.Nodes {
		afterNodes[node.ID] = node
	}
	for id, old := range beforeNodes {
		current, ok := afterNodes[id]
		if !ok {
			continue
		}
		oldStable := evalNodeInvariant(old, allowedConfig[id])
		newStable := evalNodeInvariant(current, allowedConfig[id])
		if !reflect.DeepEqual(oldStable, newStable) {
			return true
		}
	}
	beforeEdges, afterEdges := map[string]graph.EdgeView{}, map[string]graph.EdgeView{}
	for _, edge := range before.Edges {
		beforeEdges[edge.ID] = edge
	}
	for _, edge := range after.Edges {
		afterEdges[edge.ID] = edge
	}
	for id, old := range beforeEdges {
		current, ok := afterEdges[id]
		if !ok {
			continue
		}
		if old.SourceNodeID != current.SourceNodeID || old.TargetNodeID != current.TargetNodeID ||
			old.DataType != current.DataType || old.Role != current.Role {
			return true
		}
	}
	return false
}

func evalNodeInvariant(node graph.NodeView, allowConfigDerived bool) map[string]any {
	value := map[string]any{
		"id":                            node.ID,
		"node_type":                     node.NodeType,
		"source_product":                node.SourceProduct,
		"product_fact_set":              node.ProductFactSet,
		"bound_asset_id":                node.BoundAssetID,
		"preview_asset_id":              node.PreviewAssetID,
		"document_origin":               node.DocumentOrigin,
		"current_artifact_id":           node.CurrentArtifactID,
		"current_artifact_type":         node.CurrentArtifactType,
		"current_artifact_payload":      node.CurrentArtifactPayload,
		"pending_candidate_artifact_id": node.PendingCandidateArtifactID,
	}
	if !allowConfigDerived {
		value["config_status"] = node.ConfigStatus
	} else {
		// Editing config legitimately promotes seeded nodes from seed to authored.
		delete(value, "document_origin")
	}
	return value
}

func observeConfirmedEvalLibraryOperations(t *testing.T, as *agentServer, seeded seededEvalWorld, payload any, observation *evalWriteObservation) {
	t.Helper()
	container, ok := payload.(map[string]any)
	if !ok {
		observation.readback("library draft payload is not an object")
		return
	}
	operations, ok := container["operations"].([]any)
	if !ok {
		observation.readback("library draft operations are not an array")
		return
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	for _, raw := range operations {
		op, ok := raw.(map[string]any)
		if !ok {
			observation.readback("library draft operation is not an object")
			continue
		}
		operation, _ := op["operation"].(string)
		assetID, _ := op["asset_id"].(string)
		asset, err := as.svc.Library.Get(ctx, assetID)
		if err != nil {
			if apperr.IsNotFound(err) {
				observation.business("confirmed library asset is missing")
			} else {
				observation.readback("confirmed library asset: " + err.Error())
			}
			continue
		}
		target, _ := op["target"].(map[string]any)
		switch operation {
		case "rename":
			if asset.DisplayName != fmt.Sprint(target["display_name"]) {
				observation.business("confirmed asset name mismatch")
			}
		case "move":
			want, hasWant := target["folder_id"]
			if !hasWant || want == nil {
				if asset.FolderID != nil {
					observation.business("confirmed asset folder mismatch")
				}
			} else if asset.FolderID == nil || *asset.FolderID != fmt.Sprint(want) {
				observation.business("confirmed asset folder mismatch")
			}
		case "archive", "restore":
			want, _ := target["is_archived"].(bool)
			if asset.IsArchived != want {
				observation.business("confirmed asset archive state mismatch")
			}
		case "set_tags":
			wantValues, _ := target["tag_names"].([]any)
			want := make([]string, 0, len(wantValues))
			for _, value := range wantValues {
				want = append(want, fmt.Sprint(value))
			}
			have := make([]string, 0, len(asset.Tags))
			for _, tag := range asset.Tags {
				have = append(have, tag.Name)
			}
			sort.Strings(want)
			sort.Strings(have)
			if !reflect.DeepEqual(have, want) {
				observation.business("confirmed asset tags mismatch")
			}
		case "link_workflow":
			workflowID, _ := target["workflow_id"].(string)
			links, err := as.svc.Library.ObserveWorkflowLinks(ctx, workflowID, []string{asset.ID})
			if err != nil {
				observation.readback("confirmed workflow link: " + err.Error())
			} else if !links.Linked[asset.ID] {
				observation.business("confirmed workflow link missing")
			}
		default:
			observation.readback("observer does not support library operation: " + operation)
		}
	}
}

// evalFinalPersistedState is the readback evidence paired with the write grader.
// Keep the snapshot bounded to the objects exposed by the eval tools; media bytes
// and unrelated merchant rows do not belong in a trial transcript.
func evalFinalPersistedState(t *testing.T, as *agentServer, seeded seededEvalWorld) (map[string]any, []string) {
	t.Helper()
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	state := map[string]any{}
	readbackErrors := []string{}
	if seeded.GraphID != "" {
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			readbackErrors = append(readbackErrors, "graph: "+err.Error())
		} else {
			state["graph"] = live
		}
	}
	if seeded.ProductID != "" {
		product, err := as.svc.Product.Get(ctx, seeded.ProductID)
		if err != nil {
			readbackErrors = append(readbackErrors, "product: "+err.Error())
		} else {
			state["product"] = product
		}
	}
	if seeded.CreatedProductID != "" {
		product, err := as.svc.Product.Get(ctx, seeded.CreatedProductID)
		if err != nil {
			readbackErrors = append(readbackErrors, "created_product: "+err.Error())
		} else {
			state["created_product"] = product
		}
	}
	if seeded.ConvID != "" {
		var productID *string
		if seeded.Scope != "global" && seeded.ProductID != "" {
			productID = &seeded.ProductID
		}
		request, err := as.svc.GetWorkflowRunRequest(ctx, productID, seeded.ConvID, nil)
		if err != nil {
			readbackErrors = append(readbackErrors, "workflow_run_request: "+err.Error())
		} else {
			state["workflow_run_request"] = request
		}
	}
	for key, runID := range map[string]string{
		"failed_run": seeded.FailedRunID,
		"recent_run": seeded.RecentRunID,
	} {
		if runID == "" || seeded.ConvID == "" {
			continue
		}
		run, err := as.svc.WorkflowRunDetail(ctx, seeded.ConvID, runID)
		if err != nil {
			readbackErrors = append(readbackErrors, key+": "+err.Error())
			continue
		}
		state[key] = run
	}
	if seeded.Scope == "global" {
		draft, err := as.svc.Library.GetOrganizationDraft(ctx, seeded.ConvID)
		if err == nil {
			state["library_draft"] = draft
		} else if apperr.IsNotFound(err) {
			state["library_draft"] = nil
		} else {
			readbackErrors = append(readbackErrors, "library_draft: "+err.Error())
		}
		if assets, err := as.svc.ListLibraryAssets(ctx, seeded.ConvID, "", "", 100, LibraryReadOptions{IncludeArchived: true}); err == nil {
			state["library_assets"] = assets
		} else {
			readbackErrors = append(readbackErrors, "library_assets: "+err.Error())
		}
		if products, err := as.svc.ListGlobalProducts(ctx, seeded.ConvID, "", "", 100); err == nil {
			state["products"] = products
		} else {
			readbackErrors = append(readbackErrors, "products: "+err.Error())
		}
	}
	return state, readbackErrors
}

var evalOperationPath = regexp.MustCompile(`^(operations|library_payload\.operations)\[([0-9]+)\]\.(.+)$`)
var evalPathPart = regexp.MustCompile(`[^.\[\]]+`)

func evalValueAt(value any, path string) any {
	for _, key := range evalPathPart.FindAllString(path, -1) {
		switch item := value.(type) {
		case map[string]any:
			value = item[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(item) {
				return nil
			}
			value = item[i]
		default:
			return nil
		}
	}
	return value
}

func evalMatchesPaths(value any, match map[string]any) bool {
	groups := map[string]map[string]map[string]any{}
	for path, expected := range match {
		if parts := evalOperationPath.FindStringSubmatch(path); parts != nil {
			if groups[parts[1]] == nil {
				groups[parts[1]] = map[string]map[string]any{}
			}
			if groups[parts[1]][parts[2]] == nil {
				groups[parts[1]][parts[2]] = map[string]any{}
			}
			groups[parts[1]][parts[2]][parts[3]] = expected
		} else if !evalEqual(path, evalValueAt(value, path), expected) {
			return false
		}
	}
	for path, patterns := range groups {
		actual, ok := evalValueAt(value, path).([]any)
		if !ok || len(actual) != len(patterns) {
			return false
		}
		constraints := make([]map[string]any, 0, len(patterns))
		for _, pattern := range patterns {
			constraints = append(constraints, pattern)
		}
		var assign func(int, []any, map[string]string) bool
		assign = func(index int, remaining []any, bindings map[string]string) bool {
			if index == len(constraints) {
				return true
			}
			for i, item := range remaining {
				next := map[string]string{}
				for key, value := range bindings {
					next[key] = value
				}
				matched := true
				for key, expected := range constraints[index] {
					actual := evalValueAt(item, key)
					if variable, ok := expected.(string); ok && strings.HasPrefix(variable, "$") {
						text, ok := actual.(string)
						if !ok || text == "" {
							matched = false
							break
						}
						if bound, exists := next[variable]; exists {
							if bound != text {
								matched = false
								break
							}
						} else {
							for _, bound := range next {
								if bound == text {
									matched = false
								}
							}
							next[variable] = text
						}
					} else if !evalEqual(key, actual, expected) {
						matched = false
						break
					}
				}
				if !matched {
					continue
				}
				rest := append([]any{}, remaining[:i]...)
				rest = append(rest, remaining[i+1:]...)
				if assign(index+1, rest, next) {
					return true
				}
			}
			return false
		}
		if !assign(0, actual, map[string]string{}) {
			return false
		}
	}
	return true
}

func evalEqual(path string, actual, expected any) bool {
	encode := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if strings.HasSuffix(path, ".nodes") || path == "nodes" {
		a, aok := actual.([]any)
		b, bok := expected.([]any)
		if aok && bok {
			aa, bb := []string{}, []string{}
			for _, v := range a {
				aa = append(aa, encode(v))
			}
			for _, v := range b {
				bb = append(bb, encode(v))
			}
			sort.Strings(aa)
			sort.Strings(bb)
			return reflect.DeepEqual(aa, bb)
		}
	}
	if strings.HasSuffix(path, "node_ids") || strings.HasSuffix(path, "reference_asset_ids") || strings.HasSuffix(path, "tag_names") {
		a, aok := actual.([]any)
		b, bok := expected.([]any)
		if aok && bok {
			aa, bb := []string{}, []string{}
			for _, v := range a {
				aa = append(aa, encode(v))
			}
			for _, v := range b {
				bb = append(bb, encode(v))
			}
			sort.Strings(aa)
			sort.Strings(bb)
			return reflect.DeepEqual(aa, bb)
		}
	}
	return encode(actual) == encode(expected)
}

func TestEvalPersistedConfigRejectsWrongTargetAndValue(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l2")
	if err != nil {
		t.Fatal(err)
	}
	var task EvalTask
	for _, candidate := range tasks {
		if candidate.ID == "graph-editing-update-node-config" {
			task = candidate
		}
	}
	for i, test := range []struct {
		ref, goal, imageType string
		extraDelete, pass    bool
	}{{"node-prompt-2", "白底棚拍", "hero", false, false}, {"node-prompt-1", "错误目标", "hero", false, false}, {"node-prompt-1", "白底棚拍", "detail", false, false}, {"node-prompt-1", "白底棚拍", "hero", true, false}, {"node-prompt-1", "白底棚拍", "hero", false, true}} {
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"base_graph_revision": live.Revision, "summary": "eval config", "operations": []any{map[string]any{"op": "update_node_config", "node_ref": seeded.NodeIDs[test.ref], "config": map[string]any{"image_type_key": test.imageType, "prompt": map[string]any{"design_goal": test.goal}}}}})
		if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
			t.Fatal(err)
		}
		if test.extraDelete {
			raw, _ := json.Marshal(map[string]any{"base_graph_revision": live.Revision + 1, "summary": "extra delete", "operations": []any{map[string]any{"op": "delete_node", "node_ref": seeded.NodeIDs["node-image-2"]}}})
			if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
				t.Fatal(err)
			}
		}
		errors := gradeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
		if (len(errors) == 0) != test.pass {
			t.Fatal(fmt.Sprintf("case %d: %v", i, errors))
		}
	}
}

func TestEvalPersistedProposalAndLibraryContent(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l2")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"graph-editing-propose-scene-shot", "media-library-organization-rename-listed-asset"} {
		var task EvalTask
		for _, candidate := range tasks {
			if candidate.ID == id {
				task = candidate
			}
		}
		if task.ID == "" {
			t.Fatal("missing task", id)
		}
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		for _, valid := range []bool{false, true} {
			var params map[string]any
			for _, call := range task.Reference.ScriptedCalls {
				if call.Name == task.Expect.Writes[0].Tool {
					if err := json.Unmarshal(call.Params, &params); err != nil {
						t.Fatal(err)
					}
				}
			}
			if task.Skill == "graph-editing" {
				live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
				if err != nil {
					t.Fatal(err)
				}
				params["base_graph_revision"] = live.Revision
				ops := params["operations"].([]any)
				if !valid {
					ops[1].(map[string]any)["config"].(map[string]any)["image_type_key"] = "detail"
				}
				for _, operation := range ops {
					op := operation.(map[string]any)
					for _, key := range []string{"source_ref", "target_ref"} {
						ref, _ := op[key].(string)
						if actual := seeded.NodeIDs[ref]; actual != "" {
							op[key] = actual
						}
					}
				}
				raw, _ := json.Marshal(params)
				if _, err := as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
					t.Fatal(err)
				}
			} else {
				payload := params["library_payload"].(map[string]any)
				op := payload["operations"].([]any)[0].(map[string]any)
				op["asset_id"] = seeded.AssetIDs[op["asset_id"].(string)]
				if !valid {
					op["target"].(map[string]any)["display_name"] = "错误名称"
				}
				raw, _ := json.Marshal(payload)
				if _, err := as.svc.Library.AppendOrganizationDraftRevision(ctx, seeded.ConvID, raw, "", ""); err != nil {
					t.Fatal(err)
				}
			}
			failures := gradeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
			if (len(failures) == 0) != valid {
				t.Fatalf("%s valid=%t errors=%v", id, valid, failures)
			}
			if task.Skill == "graph-editing" {
				if _, err := as.svc.DiscardProposalTool(ctx, seeded.ConvID, "", clockid.New()); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestEvalPersistedOperationL1GraphCoverage(t *testing.T) {
	ids := []string{
		"graph-editing-delete-one-node",
		"graph-editing-disconnect-edge",
		"graph-editing-move-node-positions",
		"graph-editing-move-node-to-group",
		"graph-editing-rename-group",
		"graph-editing-rename-node",
		"graph-editing-update-node-config",
		"graph-editing-discard-pending-proposal",
		"graph-editing-dissolve-and-reorder",
		"graph-editing-negative-batch-direct-apply",
		"graph-editing-negative-delete-all-nodes",
		"graph-editing-propose-scene-shot",
		"workflow-run-request-negative-off-topic-delete-graph",
	}
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			as := newEvalHostServer(t)
			task := evalTaskByID(t, tasks, id)
			seeded := seedEvalWorld(t, as, task, worlds[task.World])
			ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
			var params map[string]any
			for _, call := range task.Reference.ScriptedCalls {
				if call.Name == task.Expect.Writes[0].Tool {
					if err := json.Unmarshal(call.Params, &params); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if task.Expect.Writes[0].Tool == "discard_workflow_proposal_v1" {
				live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
				if err != nil {
					t.Fatal(err)
				}
				proposal := map[string]any{"base_graph_revision": live.Revision, "summary": "待丢弃", "operations": []any{
					map[string]any{"op": "rename_node", "node_ref": seeded.NodeIDs["node-prompt-1"], "title": "待丢弃"},
				}}
				raw, _ := json.Marshal(proposal)
				if _, err := as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
					t.Fatal(err)
				}
				if _, err := as.svc.DiscardProposalTool(ctx, seeded.ConvID, "", clockid.New()); err != nil {
					t.Fatal(err)
				}
			} else {
				if params == nil {
					t.Fatal("reference write params missing")
				}
				if live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID); err == nil {
					params["base_graph_revision"] = live.Revision
				}
				raw := evalRemapReferenceIDs(t, params, seeded)
				var callErr error
				switch task.Expect.Writes[0].Tool {
				case "apply_graph_change_set_v1":
					_, callErr = as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New())
				case "propose_graph_change_set_v1":
					_, callErr = as.svc.ProposeGraphTool(ctx, seeded.ConvID, raw, clockid.New())
				default:
					t.Fatalf("unexpected graph coverage tool %s", task.Expect.Writes[0].Tool)
				}
				if callErr != nil {
					t.Fatal(callErr)
				}
			}
			observation := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
			if len(observation.Errors) != 0 || len(observation.ReadbackErrors) != 0 {
				t.Fatalf("business=%v readback=%v", observation.Errors, observation.ReadbackErrors)
			}
			if task.Expect.Writes[0].Tool == "propose_graph_change_set_v1" &&
				(id == "graph-editing-dissolve-and-reorder" || id == "graph-editing-propose-scene-shot") {
				live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
				if err != nil || live.PendingProposal == nil {
					t.Fatalf("pending proposal missing after positive observation: %v", err)
				}
				if _, err := as.svc.Graph.ConfirmProposal(ctx, seeded.ProductID, seeded.GraphID, live.PendingProposal.ID); err != nil {
					t.Fatal(err)
				}
				finalObservation := observeEvalFinalWrites(t, as, seeded, task.Expect.Writes, seeded.InitialGraph)
				if len(finalObservation.Errors) != 0 || len(finalObservation.ReadbackErrors) != 0 {
					t.Fatalf("confirmed business=%v readback=%v", finalObservation.Errors, finalObservation.ReadbackErrors)
				}
			}
		})
	}
}

func TestEvalPersistedOperationReverseObservations(t *testing.T) {
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("missing-write-is-business-failure", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "graph-editing-rename-node")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		observation := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
		if len(observation.ReadbackErrors) != 0 || len(observation.Errors) == 0 {
			t.Fatalf("missing write classification business=%v readback=%v", observation.Errors, observation.ReadbackErrors)
		}
	})
	t.Run("wrong-target-is-business-failure", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "graph-editing-rename-node")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		wrong := map[string]any{"base_graph_revision": before.Revision, "summary": "wrong target", "operations": []any{
			map[string]any{"op": "rename_node", "node_ref": seeded.NodeIDs["node-prompt-2"], "title": "新标题"},
		}}
		raw, _ := json.Marshal(wrong)
		if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
			t.Fatal(err)
		}
		observation := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
		if len(observation.ReadbackErrors) != 0 || len(observation.Errors) == 0 {
			t.Fatalf("wrong target classification business=%v readback=%v", observation.Errors, observation.ReadbackErrors)
		}
	})
	t.Run("extra-write-is-business-failure", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "graph-editing-move-node-positions")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		before, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		first := map[string]any{"base_graph_revision": before.Revision, "summary": "move", "operations": []any{
			map[string]any{"op": "move_nodes", "nodes": []any{[]any{"node-prompt-1", 640, 240}, []any{"node-image-1", 920, 240}}},
		}}
		if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, evalRemapReferenceIDs(t, first, seeded), clockid.New()); err != nil {
			t.Fatal(err)
		}
		middle, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		second := map[string]any{"base_graph_revision": middle.Revision, "summary": "extra", "operations": []any{
			map[string]any{"op": "rename_node", "node_ref": seeded.NodeIDs["node-prompt-2"], "title": "额外变更"},
		}}
		raw, _ := json.Marshal(second)
		if _, err := as.svc.ApplyGraphTool(ctx, seeded.ConvID, raw, clockid.New()); err != nil {
			t.Fatal(err)
		}
		observation := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
		if len(observation.ReadbackErrors) != 0 || len(observation.Errors) == 0 {
			t.Fatalf("extra write classification business=%v readback=%v", observation.Errors, observation.ReadbackErrors)
		}
	})
	t.Run("out-of-bound-persisted-value-is-business-failure", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "graph-editing-move-node-positions")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		if _, err := as.pool.Exec(context.Background(), `UPDATE workflow_graph_nodes SET position_x = $2, position_y = $3 WHERE id = $1`, seeded.NodeIDs["node-prompt-1"], -1, 99999); err != nil {
			t.Fatal(err)
		}
		observation := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
		if len(observation.ReadbackErrors) != 0 || len(observation.Errors) == 0 {
			t.Fatalf("out-of-bound classification business=%v readback=%v", observation.Errors, observation.ReadbackErrors)
		}
	})
}

func TestEvalPersistedLibraryOperationCoverage(t *testing.T) {
	ids := []string{
		"media-library-organization-archive-asset",
		"media-library-organization-batch-rename",
		"media-library-organization-inspect-and-rename-asset",
		"media-library-organization-inspect-before-archive",
		"media-library-organization-link-workflow",
		"media-library-organization-move-selected-asset",
		"media-library-organization-move-to-root",
		"media-library-organization-rename-listed-asset",
		"media-library-organization-restore-asset",
		"media-library-organization-set-tags",
	}
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			as := newEvalHostServer(t)
			task := evalTaskByID(t, tasks, id)
			seeded := seedEvalWorld(t, as, task, worlds[task.World])
			ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
			var params map[string]any
			for _, call := range task.Reference.ScriptedCalls {
				if call.Name == task.Expect.Writes[0].Tool {
					if err := json.Unmarshal(call.Params, &params); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
			if params == nil {
				t.Fatal("reference draft params missing")
			}
			var remapped map[string]any
			if err := json.Unmarshal(evalRemapReferenceIDs(t, params, seeded), &remapped); err != nil {
				t.Fatal(err)
			}
			payload, ok := remapped["library_payload"].(map[string]any)
			if !ok {
				t.Fatal("library payload missing")
			}
			raw, _ := json.Marshal(payload)
			draft, err := as.svc.Library.AppendOrganizationDraftRevision(ctx, seeded.ConvID, raw, "", "")
			if err != nil {
				t.Fatal(err)
			}
			pending := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
			if len(pending.Errors) != 0 || len(pending.ReadbackErrors) != 0 {
				t.Fatalf("pending business=%v readback=%v", pending.Errors, pending.ReadbackErrors)
			}
			if _, err := as.svc.ConfirmLibraryDraftHTTP(ctx, seeded.ConvID, draft.CurrentRevision.Version, clockid.New()); err != nil {
				t.Fatal(err)
			}
			confirmed := observeEvalFinalWrites(t, as, seeded, task.Expect.Writes, seeded.InitialGraph)
			if len(confirmed.Errors) != 0 || len(confirmed.ReadbackErrors) != 0 {
				t.Fatalf("confirmed business=%v readback=%v", confirmed.Errors, confirmed.ReadbackErrors)
			}
		})
	}
}

type evalPersistedReferenceCase struct {
	taskID string
	status string
}

var evalPersistedReferenceCases = []evalPersistedReferenceCase{
	{taskID: "media-library-organization-negative-off-topic-run", status: "positive-write-readback"},
	{taskID: "product-intake-expand-existing-intake", status: "positive-write-readback"},
	{taskID: "product-intake-finalize-recommended-set", status: "positive-write-readback"},
	{taskID: "product-intake-finalize-scene-and-spec", status: "positive-write-readback"},
	{taskID: "run-diagnosis-retry-after-global-diagnosis", status: "positive-write-readback"},
	{taskID: "run-diagnosis-retry-after-product-diagnosis", status: "positive-write-readback"},
	{taskID: "workflow-run-request-force-rewrite", status: "positive-write-readback"},
	{taskID: "workflow-run-request-global-retry", status: "positive-write-readback"},
	{taskID: "workflow-run-request-negative-immediate-start", status: "positive-write-readback"},
	{taskID: "workflow-run-request-retry-failed-run", status: "positive-write-readback"},
	{taskID: "workflow-run-request-run-from-global", status: "positive-write-readback"},
	{taskID: "workflow-run-request-run-selection", status: "positive-write-readback"},
	{taskID: "workflow-run-request-run-single-node", status: "positive-write-readback"},
	{taskID: "workflow-run-request-run-to-node", status: "positive-write-readback"},
}

func TestEvalPersistedNonGraphOperationCoverage(t *testing.T) {
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l1")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("request_workflow_run", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "workflow-run-request-run-current-workflow")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := parseRunScopeSpec("graph", nil, nil, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := as.svc.CreateWorkflowRunRequest(ctx, seeded.ConvID, seeded.GraphID, "eval-request", "eval-step", live.Revision, nil, nil, spec); err != nil {
			t.Fatal(err)
		}
		assertEvalWriteObservationEmpty(t, observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes))
	})
	t.Run("request_global_workflow_run", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "workflow-run-request-global-node-run")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		nodeID := seeded.NodeIDs["node-prompt-1"]
		spec, err := parseRunScopeSpec("node", &nodeID, nil, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := as.svc.CreateGlobalWorkflowRunRequest(ctx, seeded.ConvID, seeded.ProductID, seeded.GraphID, "eval-global-request", "eval-step", live.Revision, nil, nil, spec); err != nil {
			t.Fatal(err)
		}
		assertEvalWriteObservationEmpty(t, observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes))
	})
	t.Run("finalize_product_intake", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "product-intake-finalize-explicit-minimal-set")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		var input struct {
			Selection json.RawMessage `json:"selection"`
			AssetIDs  []string        `json:"reference_asset_ids"`
		}
		for _, call := range task.Reference.ScriptedCalls {
			if call.Name == task.Expect.Writes[0].Tool {
				if err := json.Unmarshal(call.Params, &input); err != nil {
					t.Fatal(err)
				}
			}
		}
		assetIDs := make([]string, 0, len(input.AssetIDs))
		for _, fixtureID := range input.AssetIDs {
			assetIDs = append(assetIDs, seeded.AssetIDs[fixtureID])
		}
		if _, err := as.svc.FinalizeProductIntake(ctx, seeded.ConvID, "eval-intake", input.Selection, assetIDs); err != nil {
			t.Fatal(err)
		}
		assertEvalWriteObservationEmpty(t, observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes))
	})
	t.Run("create_product_workspace", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "product-intake-create-named-workspace")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		launch, err := as.svc.LaunchWorkspaceFromGlobal(ctx, seeded.ConvID, "秋季咖啡杯", "eval-workspace")
		if err != nil {
			t.Fatal(err)
		}
		seeded.CreatedProductID = launch.ProductID
		assertEvalWriteObservationEmpty(t, observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes))
	})
	t.Run("cancel_workflow_run", func(t *testing.T) {
		as := newEvalHostServer(t)
		task := evalTaskByID(t, tasks, "workflow-run-request-cancel-running-run")
		seeded := seedEvalWorld(t, as, task, worlds[task.World])
		ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
		if _, err := as.svc.CancelRunTool(ctx, seeded.ConvID, seeded.RecentRunID, "eval-cancel"); err != nil {
			t.Fatal(err)
		}
		assertEvalWriteObservationEmpty(t, observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes))
	})
	for _, referenceCase := range evalPersistedReferenceCases {
		t.Run(referenceCase.taskID, func(t *testing.T) {
			task := evalTaskByID(t, tasks, referenceCase.taskID)
			as := newEvalHostServer(t)
			seeded := seedEvalWorld(t, as, task, worlds[task.World])
			runEvalPersistedReferenceWrite(t, as, task, seeded)
		})
	}
}

func assertEvalWriteObservationEmpty(t *testing.T, observation evalWriteObservation) {
	t.Helper()
	if len(observation.Errors) != 0 || len(observation.ReadbackErrors) != 0 {
		t.Fatalf("business=%v readback=%v", observation.Errors, observation.ReadbackErrors)
	}
}

func runEvalPersistedReferenceWrite(t *testing.T, as *agentServer, task EvalTask, seeded seededEvalWorld) {
	t.Helper()
	if len(task.Expect.Writes) != 1 {
		t.Fatalf("reference coverage expects one write, got %d", len(task.Expect.Writes))
	}
	expected := task.Expect.Writes[0]
	params := evalReferenceWriteParams(t, task, expected.Tool)
	remapped := evalRemapReferenceIDs(t, params, seeded)
	var actual map[string]any
	if err := json.Unmarshal(remapped, &actual); err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithMerchantID(context.Background(), auth.MustDevMerchantID(t, as.db))
	switch expected.Tool {
	case "request_workflow_run_v1", "request_global_workflow_run_v1":
		live, err := as.svc.Graph.Get(ctx, seeded.ProductID, seeded.GraphID)
		if err != nil {
			t.Fatal(err)
		}
		actual["expected_workflow_revision"] = live.Revision
		if callErr := executeEvalReferenceRunRequest(t, as, task, seeded, ctx, actual); callErr != nil {
			t.Fatalf("reference %s write failed: %v", expected.Tool, callErr)
		}
	case "finalize_product_intake_v1":
		if err := executeEvalReferenceIntake(t, as, seeded, ctx, actual); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("reference coverage does not support %s", expected.Tool)
	}
	observation := observeEvalPersistedWrites(t, as, seeded, task.Expect.Writes)
	assertEvalWriteObservationEmpty(t, observation)
}

func evalReferenceWriteParams(t *testing.T, task EvalTask, tool string) map[string]any {
	t.Helper()
	var params map[string]any
	for _, call := range task.Reference.ScriptedCalls {
		if call.Name != tool {
			continue
		}
		if err := json.Unmarshal(call.Params, &params); err != nil {
			t.Fatalf("%s reference params: %v", task.ID, err)
		}
		// The final matching call is the operation whose persisted effect is graded.
	}
	if params == nil {
		t.Fatalf("%s missing scripted call for %s", task.ID, tool)
	}
	return params
}

func executeEvalReferenceRunRequest(t *testing.T, as *agentServer, task EvalTask, seeded seededEvalWorld, ctx context.Context, params map[string]any) error {
	t.Helper()
	expectedRevision := evalReferenceInt(params, "expected_workflow_revision", seeded.InitialGraph.Revision)
	scope, _ := params["scope"].(string)
	force, _ := params["force"].(bool)
	documentAction, _ := params["document_action"].(string)
	var nodeID *string
	if value, ok := params["node_id"].(string); ok && value != "" {
		nodeID = &value
	}
	nodeIDs := stringList(params["node_ids"])
	spec, err := parseRunScopeSpec(scope, nodeID, nodeIDs, force, documentAction)
	if err != nil {
		return err
	}
	sourceRunID := evalReferenceOptionalString(params["source_run_id"])
	idempotencyKey := "eval-" + task.ID
	sourceStepID := "eval-reference-" + task.ID
	if task.Scope == "global" {
		productID := evalReferenceString(params, "product_id", seeded.ProductID)
		workflowID := evalReferenceString(params, "workflow_id", seeded.GraphID)
		_, err = as.svc.CreateGlobalWorkflowRunRequest(ctx, seeded.ConvID, productID, workflowID, idempotencyKey, sourceStepID, expectedRevision, nil, sourceRunID, spec)
		return err
	}
	_, err = as.svc.CreateWorkflowRunRequest(ctx, seeded.ConvID, seeded.GraphID, idempotencyKey, sourceStepID, expectedRevision, nil, sourceRunID, spec)
	return err
}

func executeEvalReferenceIntake(t *testing.T, as *agentServer, seeded seededEvalWorld, ctx context.Context, params map[string]any) error {
	selection, ok := params["selection"]
	if !ok {
		return fmt.Errorf("finalize_product_intake_v1 reference selection missing")
	}
	selectionRaw, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	assetIDs := stringList(params["reference_asset_ids"])
	actualAssetIDs := make([]string, 0, len(assetIDs))
	for _, actualID := range assetIDs {
		if actualID == "" {
			return fmt.Errorf("reference intake asset was empty")
		}
		actualAssetIDs = append(actualAssetIDs, actualID)
	}
	_, err = as.svc.FinalizeProductIntake(ctx, seeded.ConvID, "eval-"+seeded.ProductID, selectionRaw, actualAssetIDs)
	return err
}

func evalReferenceInt(params map[string]any, key string, fallback int) int {
	value, ok := params[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return fallback
	}
}

func evalReferenceString(params map[string]any, key, fallback string) string {
	value, ok := params[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func evalReferenceOptionalString(value any) *string {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	return &text
}

func evalRemapReferenceIDs(t *testing.T, value map[string]any, seeded seededEvalWorld) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for fixture, actual := range evalFixtureToActualIDs(seeded) {
		if fixture != "" && actual != "" {
			text = strings.ReplaceAll(text, strconv.Quote(fixture), strconv.Quote(actual))
		}
	}
	return json.RawMessage(text)
}
