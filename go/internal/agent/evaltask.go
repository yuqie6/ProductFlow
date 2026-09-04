package agent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EvalTask is the language-neutral Agent eval task JSON (subset used by Go L2/L6).
type EvalTask struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id"`
	Skill         string         `json:"skill"`
	Scope         string         `json:"scope"`
	Suite         string         `json:"suite"`
	CaseType      string         `json:"case_type"`
	Layers        []string       `json:"layers"`
	Utterances    []string       `json:"utterances"`
	World         string         `json:"world"`
	PageContext   map[string]any `json:"page_context"`
	Expect        EvalExpect     `json:"expect"`
	Origin        string         `json:"origin"`
	Inject        *EvalInject    `json:"inject"`
}

// EvalExpect is the per-layer expectation block.
type EvalExpect struct {
	Terminal []string         `json:"terminal"`
	Tools    EvalNameSet      `json:"tools"`
	Ops      EvalNameSet      `json:"ops"`
	Writes   []map[string]any `json:"writes"`
	State    *EvalStateExpect `json:"state"`
}

// EvalNameSet is required/forbidden tool or op names.
type EvalNameSet struct {
	Required  []string `json:"required"`
	Forbidden []string `json:"forbidden"`
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

// EvalInject overlays adversarial or fault payloads onto a seeded world.
type EvalInject struct {
	FirstWrite409 string            `json:"first_write_409"`
	Write409Count int               `json:"write_409_count"`
	Payload       map[string]string `json:"payload"`
}

// EvalWorld is a named ProductFlow world used to seed PostgreSQL or the TS stub.
type EvalWorld struct {
	SchemaVersion   int                `json:"schema_version"`
	Name            string             `json:"name"`
	Intake          json.RawMessage    `json:"intake"`
	BirthExpandable bool               `json:"birth_expandable"`
	LiveGraph       EvalWorldGraph     `json:"live_graph"`
	FailedRun       *EvalFailedRun     `json:"failed_run"`
	ListedAssets    []EvalListedAsset  `json:"listed_assets"`
	ListedFolders   []EvalListedFolder `json:"listed_folders"`
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
	ID       string `json:"id"`
	NodeType string `json:"node_type"`
	Title    string `json:"title"`
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
		if world.Name == "" {
			return nil, fmt.Errorf("%s: missing world name", file)
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
		if task.ID == "" || task.Skill == "" || task.World == "" {
			return nil, worlds, fmt.Errorf("%s: missing id, skill, or world", file)
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
	return json.Unmarshal(raw, dest)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
