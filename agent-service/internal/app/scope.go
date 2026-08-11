package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const scopeSchemaVersion = 1

type Scope struct {
	SchemaVersion   int    `json:"schema_version"`
	ConversationID  string `json:"conversation_id"`
	ProductID       string `json:"product_id"`
	WorkflowDraftID string `json:"workflow_draft_id"`
	RunID           string `json:"run_id"`
}

func ensureScope(dataRoot string, expected Scope) (string, string, error) {
	if expected.SchemaVersion != scopeSchemaVersion {
		return "", "", fmt.Errorf("unsupported scope schema version %d", expected.SchemaVersion)
	}
	directory := filepath.Join(dataRoot, "conversations", expected.ConversationID)
	workspace := filepath.Join(directory, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return "", "", fmt.Errorf("create conversation workspace: %w", err)
	}
	scopePath := filepath.Join(directory, "scope.json")
	if data, err := os.ReadFile(scopePath); err == nil {
		var stored Scope
		if err := json.Unmarshal(data, &stored); err != nil {
			return "", "", fmt.Errorf("decode persisted conversation scope: %w", err)
		}
		if stored != expected {
			return "", "", errors.New("persisted conversation scope conflicts with ProductFlow contract")
		}
		return filepath.Join(directory, "agent.db"), workspace, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("read persisted conversation scope: %w", err)
	}
	encoded, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return "", "", err
	}
	temporary, err := os.CreateTemp(directory, ".scope-*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("create temporary scope file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := os.Rename(temporaryName, scopePath); err != nil {
		return "", "", fmt.Errorf("persist conversation scope: %w", err)
	}
	return filepath.Join(directory, "agent.db"), workspace, nil
}
