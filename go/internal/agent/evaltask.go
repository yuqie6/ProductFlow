package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	evalScopes = map[string]struct{}{
		"product_workflow": {},
		"global":           {},
	}
	evalSuites = map[string]struct{}{
		"regression":  {},
		"capability":  {},
		"adversarial": {},
	}
	evalCaseTypes = map[string]struct{}{
		"positive": {},
		"negative": {},
	}
	evalLayers = map[string]struct{}{
		"l0": {}, "l1": {}, "l2": {}, "l3": {}, "l4": {}, "l5": {}, "l6": {},
	}
	evalSplits = map[string]struct{}{
		"held_in":  {},
		"held_out": {},
	}
	evalTerminals = map[string]struct{}{
		"queued":                {},
		"running":               {},
		"requires_input":        {},
		"awaiting_confirmation": {},
		"succeeded":             {},
		"failed":                {},
		"cancel_requested":      {},
		"canceled":              {},
		"unknown":               {},
	}
	evalRecentRunStatuses = map[string]struct{}{
		"queued":    {},
		"running":   {},
		"succeeded": {},
		"failed":    {},
		"cancelled": {},
		"unknown":   {},
	}
)

// EvalTask is the language-neutral Agent eval task JSON used by Go L2/L6.
type EvalTask struct {
	ObservabilityBlocker string         `json:"observability_blocker,omitempty"`
	SchemaVersion        int            `json:"schema_version"`
	ID                   string         `json:"id"`
	Skill                string         `json:"skill"`
	Scope                string         `json:"scope"`
	Suite                string         `json:"suite"`
	CaseType             string         `json:"case_type"`
	Layers               []string       `json:"layers"`
	Split                string         `json:"split"`
	Utterances           []string       `json:"utterances"`
	World                string         `json:"world"`
	PageContext          map[string]any `json:"page_context"`
	Expect               EvalExpect     `json:"expect"`
	Origin               string         `json:"origin"`
	Reference            EvalReference  `json:"reference"`
	Inject               *EvalInject    `json:"inject"`
	UserSim              *EvalUserSim   `json:"user_sim"`
}

// EvalExpect is the per-layer expectation block.
type EvalExpect struct {
	Terminal []string          `json:"terminal"`
	Tools    EvalNameSet       `json:"tools"`
	Ops      EvalNameSet       `json:"ops"`
	Writes   []EvalWriteExpect `json:"writes"`
	State    *EvalStateExpect  `json:"state"`
	Question json.RawMessage   `json:"question"`
	Budget   *EvalBudgetExpect `json:"budget"`
	Rubric   json.RawMessage   `json:"rubric"`
}

// EvalWriteExpect is one expected tool write.
type EvalWriteExpect struct {
	Tool  string         `json:"tool"`
	Match map[string]any `json:"match"`
}

// EvalBudgetExpect is an optional live-run budget.
type EvalBudgetExpect struct {
	MaxToolCalls  *int `json:"max_tool_calls"`
	MaxTokens     *int `json:"max_tokens"`
	MaxDurationMS *int `json:"max_duration_ms"`
}

// EvalNameSet is required/forbidden tool or op names.
type EvalNameSet struct {
	Required  []string          `json:"required"`
	Forbidden []string          `json:"forbidden"`
	Reads     []EvalWriteExpect `json:"reads,omitempty"`
}

// EvalStateExpect is the PostgreSQL-facing L2 grader contract.
type EvalStateExpect struct {
	NodeTitles           map[string]string `json:"node_titles"`
	MinNodeCount         *int              `json:"min_node_count"`
	MinRevision          *int              `json:"min_revision"`
	PendingProposals     *int              `json:"pending_proposals"`
	PendingRunRequests   *int              `json:"pending_run_requests"`
	RequireSourceRunID   *bool             `json:"require_source_run_id"`
	IntakeImageTypeKeys  []string          `json:"intake_image_type_keys"`
	PendingLibraryDrafts *int              `json:"pending_library_drafts"`
	FailedRunPresent     *bool             `json:"failed_run_present"`
	ProductNameContains  string            `json:"product_name_contains"`
}

// EvalReference is the scripted L0 contract; live pass still uses expect graders.
type EvalReference struct {
	ScriptedCalls []EvalReferenceCall `json:"scripted_calls"`
	Repair        *EvalRepair         `json:"repair"`
}

// EvalReferenceCall is one scripted tool invocation.
type EvalReferenceCall struct {
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params"`
}

// EvalRepair records an illegal first attempt and the repaired retry.
type EvalRepair struct {
	ToolName       string          `json:"tool_name"`
	IllegalParams  json.RawMessage `json:"illegal_params"`
	RepairedParams json.RawMessage `json:"repaired_params"`
}

// EvalUserSim is the L3 simulated-user contract.
type EvalUserSim struct {
	Persona         string              `json:"persona"`
	HiddenGoal      string              `json:"hidden_goal"`
	Facts           map[string]string   `json:"facts"`
	Policy          string              `json:"policy"`
	MaxTurns        int                 `json:"max_turns"`
	ScriptedAnswers []EvalUserSimAnswer `json:"scripted_answers"`
}

// EvalUserSimAnswer is one scripted or live simulated-user turn.
type EvalUserSimAnswer struct {
	When            string          `json:"when"`
	Action          string          `json:"action"`
	Text            string          `json:"text"`
	Answer          json.RawMessage `json:"answer"`
	AuthorizeWrites bool            `json:"authorize_writes,omitempty"`
}

// EvalInject overlays adversarial or fault payloads onto a seeded world.
type EvalInject struct {
	FirstWrite409 string               `json:"first_write_409"`
	Write409Count int                  `json:"write_409_count"`
	ReadError     *EvalInjectReadError `json:"read_error"`
	Payload       map[string]string    `json:"payload"`
}

// EvalInjectReadError forces a read tool to fail.
type EvalInjectReadError struct {
	Tool   string          `json:"tool"`
	Status json.RawMessage `json:"status"`
}

type evalPageContextJSON struct {
	SnapshotID       string            `json:"snapshot_id"`
	Route            string            `json:"route"`
	PageType         string            `json:"page_type"`
	ProductID        *string           `json:"product_id"`
	WorkflowID       *string           `json:"workflow_id"`
	SelectedAssetIDs []string          `json:"selected_asset_ids"`
	VisibleAssetIDs  []string          `json:"visible_asset_ids"`
	Filters          map[string]string `json:"filters"`
	WorkflowRevision *int              `json:"workflow_revision"`
	LibraryRevision  *int              `json:"library_revision"`
	Digest           string            `json:"digest"`
	CapturedAt       string            `json:"captured_at"`
}

type evalInjectPayloadJSON struct {
	DisplayName   string `json:"display_name"`
	ProductName   string `json:"product_name"`
	NodeTitle     string `json:"node_title"`
	FailureReason string `json:"failure_reason"`
	FolderTitle   string `json:"folder_title"`
}

// EvalWorld is a named ProductFlow world used to seed PostgreSQL or the TS stub.
type EvalWorld struct {
	PendingProposalID  string             `json:"pending_proposal_id,omitempty"`
	ListedWorkflowRuns []map[string]any   `json:"listed_workflow_runs,omitempty"`
	SchemaVersion      int                `json:"schema_version"`
	Name               string             `json:"name"`
	Intake             json.RawMessage    `json:"intake"`
	BirthExpandable    bool               `json:"birth_expandable"`
	LiveGraph          EvalWorldGraph     `json:"live_graph"`
	FailedRun          *EvalFailedRun     `json:"failed_run"`
	RecentRun          *EvalRecentRun     `json:"recent_run"`
	ListedAssets       []EvalListedAsset  `json:"listed_assets"`
	ListedFolders      []EvalListedFolder `json:"listed_folders"`
}

// EvalWorldGraph is the graph shape described by a world JSON file.
type EvalWorldGraph struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Revision   int              `json:"revision"`
	NodeCount  int              `json:"node_count"`
	EdgeCount  int              `json:"edge_count"`
	GroupCount int              `json:"group_count"`
	Nodes      []EvalWorldNode  `json:"nodes"`
	Edges      []EvalWorldEdge  `json:"edges"`
	Groups     []EvalWorldGroup `json:"groups"`
}

// EvalWorldNode is a world node identified by a stable eval id, not a PG UUID.
type EvalWorldNode struct {
	ID       string         `json:"id"`
	NodeType string         `json:"node_type"`
	Title    string         `json:"title"`
	Config   map[string]any `json:"config,omitempty"`
}

// EvalWorldEdge is a world edge using world node ids.
type EvalWorldEdge struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Role     string `json:"role"`
	Order    int    `json:"order"`
}

// EvalWorldGroup is a world visual group.
type EvalWorldGroup struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	MemberIDs []string `json:"member_ids"`
}

// EvalFailedRun is an optional failed GraphRun seed.
type EvalFailedRun struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	FailedNodeID string `json:"failed_node_id"`
}

// EvalRecentRun is an optional in-progress or completed GraphRun seed.
type EvalRecentRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// EvalListedAsset is a global library asset in the world.
type EvalListedAsset struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Revision    int      `json:"revision"`
	FolderID    *string  `json:"folder_id"`
	TagNames    []string `json:"tag_names"`
	IsArchived  bool     `json:"is_archived"`
}

// EvalListedFolder is a global library folder in the world.
type EvalListedFolder struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// DefaultEvalRoot returns agent-service/evals relative to this source file.
func DefaultEvalRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "agent-service", "evals"))
}

// LoadEvalWorlds reads worlds/*.json.
func LoadEvalWorlds(root string) (map[string]EvalWorld, error) {
	dir := filepath.Join(root, "worlds")
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, err
	}
	worlds := map[string]EvalWorld{}
	for _, file := range files {
		var world EvalWorld
		if err := readJSON(file, &world); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if err := validateEvalWorld(world, file); err != nil {
			return nil, err
		}
		if _, exists := worlds[world.Name]; exists {
			return nil, fmt.Errorf("duplicate eval world %s", world.Name)
		}
		worlds[world.Name] = world
	}
	return worlds, nil
}

// LoadEvalTasks reads tasks/**/*.json. If layer is non-empty, only tasks that list it are returned.
func LoadEvalTasks(root, layer string) ([]EvalTask, map[string]EvalWorld, error) {
	worlds, err := LoadEvalWorlds(root)
	if err != nil {
		return nil, nil, err
	}
	files, err := jsonFiles(filepath.Join(root, "tasks"))
	if err != nil {
		return nil, worlds, err
	}
	var tasks []EvalTask
	seen := map[string]struct{}{}
	for _, file := range files {
		var task EvalTask
		if err := readJSON(file, &task); err != nil {
			return nil, worlds, fmt.Errorf("%s: %w", file, err)
		}
		if err := validateEvalTaskDocument(task, file); err != nil {
			return nil, worlds, err
		}
		if _, exists := seen[task.ID]; exists {
			return nil, worlds, fmt.Errorf("duplicate eval task id %s", task.ID)
		}
		seen[task.ID] = struct{}{}
		if _, ok := worlds[task.World]; !ok {
			return nil, worlds, fmt.Errorf("task %s references unknown world %s", task.ID, task.World)
		}
		if layer != "" && !containsString(task.Layers, layer) {
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, worlds, nil
}

// FilterEvalTasks keeps tasks whose id or skill matches filter. Empty filter returns tasks unchanged.
func FilterEvalTasks(tasks []EvalTask, filter string) []EvalTask {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return tasks
	}
	var out []EvalTask
	for _, task := range tasks {
		if task.ID == filter || task.Skill == filter || strings.Contains(task.ID, filter) {
			out = append(out, task)
		}
	}
	return out
}

func jsonFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func readJSON(path string, dest any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeJSONBytes(raw, dest)
}

func decodeJSONBytes(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}

func decodeStrict(value any, dest any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return decodeJSONBytes(raw, dest)
}

func validateEvalWorld(world EvalWorld, file string) error {
	if world.SchemaVersion != 1 {
		return fmt.Errorf("%s: schema_version must be 1", file)
	}
	if world.Name == "" {
		return fmt.Errorf("%s: missing world name", file)
	}
	if world.FailedRun != nil && world.FailedRun.Status != "failed" {
		return fmt.Errorf("%s: invalid failed_run.status %q", file, world.FailedRun.Status)
	}
	if world.RecentRun != nil {
		if err := requireEnum("recent_run.status", world.RecentRun.Status, evalRecentRunStatuses); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	return nil
}

func validateEvalTaskDocument(task EvalTask, file string) error {
	if task.SchemaVersion != 1 {
		return fmt.Errorf("%s: schema_version must be 1", file)
	}
	if task.ID == "" || task.Skill == "" || task.World == "" {
		return fmt.Errorf("%s: missing id, skill, or world", file)
	}
	if err := requireEnum("scope", task.Scope, evalScopes); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if err := requireEnum("suite", task.Suite, evalSuites); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if err := requireEnum("case_type", task.CaseType, evalCaseTypes); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if task.Split != "" {
		if err := requireEnum("split", task.Split, evalSplits); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	if len(task.Layers) == 0 {
		return fmt.Errorf("%s: missing layers", file)
	}
	for _, layer := range task.Layers {
		if err := requireEnum("layers", layer, evalLayers); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	for _, terminal := range task.Expect.Terminal {
		if err := requireEnum("expect.terminal", terminal, evalTerminals); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
	}
	if containsString(task.Layers, "l2") && task.Expect.State == nil {
		return fmt.Errorf("%s: layer l2 requires expect.state", file)
	}
	if containsString(task.Layers, "l3") && task.UserSim == nil {
		return fmt.Errorf("%s: layer l3 requires user_sim", file)
	}
	if err := decodeStrict(task.PageContext, new(evalPageContextJSON)); err != nil {
		return fmt.Errorf("%s: page_context: %w", file, err)
	}
	if task.Inject != nil && task.Inject.Payload != nil {
		if err := decodeStrict(task.Inject.Payload, new(evalInjectPayloadJSON)); err != nil {
			return fmt.Errorf("%s: inject.payload: %w", file, err)
		}
	}
	return nil
}

func requireEnum(field, value string, allowed map[string]struct{}) error {
	if _, ok := allowed[value]; ok {
		return nil
	}
	return fmt.Errorf("invalid %s %q", field, value)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
