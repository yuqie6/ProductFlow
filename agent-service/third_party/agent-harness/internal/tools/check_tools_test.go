package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedCheckRunsExactCommandInReadOnlyOfflineSandbox(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	workspace := sandboxTestWorkspace(t)
	marker := filepath.Join(workspace, "marker.txt")
	command := "printf checked; if printf blocked > marker.txt; then exit 91; fi; test \"$(wc -l < /proc/net/route)\" -eq 1"
	if shouldMaskWSLInterop() && pathExists("/mnt/c/Users") {
		command += " && test ! -e /mnt/c/Users"
	}
	set, info, err := NewNamedCheckSet(workspace, []NamedCheck{{
		Name: "unit", Description: "unit tests", Required: true,
		Command: command,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Enabled || info.Backend != "bwrap" || len(set.RequiredNames()) != 1 {
		t.Fatalf("info = %#v, required = %#v", info, set.RequiredNames())
	}
	snapshot, err := set.Snapshot(json.RawMessage(`{"name":"unit"}`))
	if err != nil {
		t.Fatal(err)
	}
	var decoded CheckSnapshot
	if err := json.Unmarshal(snapshot, &decoded); err != nil || decoded.ConfigSHA256 == "" {
		t.Fatalf("snapshot = %s, err = %v", snapshot, err)
	}
	output, runErr := set.Tool().Handler(context.Background(), snapshot)
	if runErr != nil || !strings.Contains(output, `"name":"unit"`) {
		t.Fatalf("output = %s, err = %v", output, runErr)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sandbox wrote workspace marker: %v", err)
	}
}

func TestStrongCheckSandboxHidesHomeAndWorkspaceSecrets(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	home := sandboxTestWorkspace(t)
	t.Setenv("HOME", home)
	t.Setenv("GOPATH", filepath.Join(home, "go"))
	workspace := filepath.Join(home, "project")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	homeSecret := filepath.Join(home, ".ssh", "id_test")
	if err := os.MkdirAll(filepath.Dir(homeSecret), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(homeSecret, []byte("host-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaceSecret := filepath.Join(workspace, ".env")
	if err := os.WriteFile(workspaceSecret, []byte("workspace-secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	set, _, err := NewNamedCheckSet(workspace, []NamedCheck{{
		Name: "secrets", Command: "test ! -e " + shellQuote(homeSecret) + " && test ! -s .env",
	}})
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := set.Tool().Handler(context.Background(), json.RawMessage(`{"name":"secrets"}`))
	if runErr != nil {
		t.Fatalf("output = %s, err = %v", output, runErr)
	}
}

func TestStrongCheckSandboxLayoutIsOfflineAndSelective(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOPATH", filepath.Join(home, "go"))
	workspace := filepath.Join(home, "projects", "sample")
	for _, directory := range []string{workspace, filepath.Join(home, "go", "pkg", "mod"), filepath.Join(home, "go", "bin")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".env.example"), []byte("example"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, sandboxHome, err := strongCheckSandboxLayout(workspace, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\x00")
	for _, want := range []string{
		"--tmpfs\x00" + home,
		"--ro-bind\x00" + workspace + "\x00" + workspace,
		"--ro-bind\x00" + filepath.Join(home, "go", "pkg", "mod"),
		"--ro-bind\x00/dev/null\x00" + filepath.Join(workspace, ".env"),
		"--unshare-net",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sandbox args missing %q: %#v", want, args)
		}
	}
	if strings.Contains(joined, filepath.Join(workspace, ".env.example")) {
		t.Fatalf(".env.example must remain readable: %#v", args)
	}
	if sandboxHome != "/tmp/harness-home" {
		t.Fatalf("sandbox HOME = %q", sandboxHome)
	}
}

func TestStrongCheckSandboxMasksWSLHostMounts(t *testing.T) {
	if !pathExists("/mnt") {
		t.Skip("/mnt is not present")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "project")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	args, _, err := strongCheckSandboxLayout(workspace, true)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(args, "\x00"); !strings.Contains(joined, "--tmpfs\x00/mnt") {
		t.Fatalf("WSL mount mask missing: %#v", args)
	}
}

func TestNamedCheckRejectsConfigurationDrift(t *testing.T) {
	first := checkSetForSnapshotTest(t, NamedCheck{Name: "unit", Command: "true", TimeoutSeconds: 10})
	snapshot, err := first.Snapshot(json.RawMessage(`{"name":"unit"}`))
	if err != nil {
		t.Fatal(err)
	}
	second := checkSetForSnapshotTest(t, NamedCheck{Name: "unit", Command: "false", TimeoutSeconds: 10})
	if _, err := second.Snapshot(snapshot); !errors.Is(err, ErrCheckConfigChanged) {
		t.Fatalf("configuration drift error = %v", err)
	}
}

func TestNamedCheckContractDigestChangesWithExecutionConfig(t *testing.T) {
	first := checkSetForSnapshotTest(t, NamedCheck{Name: "unit", Description: "unit", Command: "true", TimeoutSeconds: 10, Required: true})
	same := checkSetForSnapshotTest(t, NamedCheck{Name: "unit", Description: "unit", Command: "true", TimeoutSeconds: 10, Required: true})
	changedCommand := checkSetForSnapshotTest(t, NamedCheck{Name: "unit", Description: "unit", Command: "false", TimeoutSeconds: 10, Required: true})
	changedRequirement := checkSetForSnapshotTest(t, NamedCheck{Name: "unit", Description: "unit", Command: "true", TimeoutSeconds: 10, Required: false})

	firstDigest, err := first.ContractSHA256()
	if err != nil {
		t.Fatal(err)
	}
	sameDigest, err := same.ContractSHA256()
	if err != nil {
		t.Fatal(err)
	}
	changedCommandDigest, err := changedCommand.ContractSHA256()
	if err != nil {
		t.Fatal(err)
	}
	changedRequirementDigest, err := changedRequirement.ContractSHA256()
	if err != nil {
		t.Fatal(err)
	}

	if firstDigest == "" || firstDigest != sameDigest {
		t.Fatalf("stable contract digests = %q and %q", firstDigest, sameDigest)
	}
	if firstDigest == changedCommandDigest {
		t.Fatal("named check contract ignored command drift")
	}
	if firstDigest == changedRequirementDigest {
		t.Fatal("named check contract ignored required-gate drift")
	}
}

func TestNamedCheckDigestBindsSandboxProfile(t *testing.T) {
	check := NamedCheck{Name: "unit", Command: "true", TimeoutSeconds: 10}
	digest, err := checkDigest(check)
	if err != nil {
		t.Fatal(err)
	}
	digestForProfile := func(profile string) string {
		encoded, marshalErr := json.Marshal(struct {
			Name           string `json:"name"`
			Command        string `json:"command"`
			TimeoutSeconds int    `json:"timeout_seconds"`
			SandboxProfile string `json:"sandbox_profile"`
		}{check.Name, check.Command, check.TimeoutSeconds, profile})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		sum := sha256.Sum256(encoded)
		return hex.EncodeToString(sum[:])
	}
	if digest != digestForProfile(StrongCheckSandboxProfile) {
		t.Fatal("named check digest does not include the active sandbox profile")
	}
	if digest == digestForProfile("different-profile") {
		t.Fatal("named check digest ignored sandbox profile")
	}
}

func TestNamedCheckNeverFallsBackWhenBwrapIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	set, info, err := NewNamedCheckSet(t.TempDir(), []NamedCheck{{Name: "unit", Command: "true"}})
	if err == nil || set != nil || info.Enabled || !strings.Contains(err.Error(), "需要强制 bwrap") {
		t.Fatalf("set = %#v, info = %#v, err = %v", set, info, err)
	}
}

func TestNamedCheckAlwaysRequestsReadOnlySandboxBoundary(t *testing.T) {
	check := NamedCheck{Name: "unit", Command: "printf ok", TimeoutSeconds: 10}
	digest, err := checkDigest(check)
	if err != nil {
		t.Fatal(err)
	}
	executor := &recordingCheckExecutor{}
	definition := checkDefinition{NamedCheck: check, digest: digest}
	set := &NamedCheckSet{
		checks: map[string]checkDefinition{check.Name: definition}, ordered: []checkDefinition{definition}, executor: executor,
	}
	set.tool = set.buildTool()
	if _, err := set.Tool().Handler(context.Background(), json.RawMessage(`{"name":"unit"}`)); err != nil {
		t.Fatal(err)
	}
	if executor.boundary != ExecutionSandboxReadOnly {
		t.Fatalf("boundary = %v", executor.boundary)
	}
	for _, want := range []string{"ulimit -t 15 || exit $?", "ulimit -v 2097152 || exit $?", "ulimit -u 512 || exit $?", "ulimit -n 1024 || exit $?", "ulimit -f 1048576 || exit $?", "printf ok"} {
		if !strings.Contains(executor.command, want) {
			t.Fatalf("limited command missing %q: %s", want, executor.command)
		}
	}
}

type recordingCheckExecutor struct {
	boundary ExecutionBoundary
	command  string
}

func (executor *recordingCheckExecutor) Command(ctx context.Context, command string, boundary ExecutionBoundary) *exec.Cmd {
	executor.boundary = boundary
	executor.command = command
	return exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", command)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func checkSetForSnapshotTest(t *testing.T, check NamedCheck) *NamedCheckSet {
	t.Helper()
	digest, err := checkDigest(check)
	if err != nil {
		t.Fatal(err)
	}
	definition := checkDefinition{NamedCheck: check, digest: digest}
	return &NamedCheckSet{checks: map[string]checkDefinition{check.Name: definition}, ordered: []checkDefinition{definition}}
}
