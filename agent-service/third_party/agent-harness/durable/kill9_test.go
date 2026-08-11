package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	killHelperFlag      = "HARNESS_DURABLE_KILL_HELPER"
	killHelperDatabase  = "HARNESS_DURABLE_KILL_DATABASE"
	killHelperWorkspace = "HARNESS_DURABLE_KILL_WORKSPACE"
	killHelperJob       = "HARNESS_DURABLE_KILL_JOB"
	killHelperPoint     = "HARNESS_DURABLE_KILL_POINT"
)

func TestFileEditRecoversFromRealSIGKILL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SIGKILL journal recovery test requires Linux")
	}
	for _, point := range []FaultPoint{FaultBeforeEffect, FaultAfterEffect, FaultAfterCommit} {
		t.Run(string(point), func(t *testing.T) {
			workspace := t.TempDir()
			database := filepath.Join(t.TempDir(), "jobs.db")
			path := filepath.Join(workspace, "value.txt")
			before := []byte("old\n")
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			tool, err := NewFileEditTool(workspace)
			if err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(FileEditInput{
				Path: "value.txt", ExpectedSHA256: digestBytes(before), OldText: "old", NewText: "new",
			})
			job := submitAndClose(t, database, tool, JobSpec{
				Name: "kill edit", Steps: []StepSpec{{Tool: tool.Name(), Input: input}},
			})

			runWorkerUntilSIGKILL(t, database, workspace, job.ID, point)
			recovery := openTestEngine(t, database, Options{
				Owner: "recovery-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
			}, tool)
			waitForPersistedLeasesToExpire(t, recovery, job.ID)
			job, err = recovery.Run(t.Context(), job.ID)
			if err != nil || job.Status != JobSucceeded {
				t.Fatalf("job = %#v, err = %v", job, err)
			}
			content, err := os.ReadFile(path)
			if err != nil || string(content) != "new\n" {
				t.Fatalf("content = %q, err = %v", content, err)
			}
			attempts, err := recovery.Attempts(t.Context(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 || attempts[0].Status != AttemptSucceeded {
				t.Fatalf("attempts = %#v", attempts)
			}
			events, err := recovery.Events(t.Context(), job.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if countEvent(events, "attempt.succeeded") != 1 || countEvent(events, "job.succeeded") != 1 {
				t.Fatalf("events = %#v", events)
			}
		})
	}
}

func TestOpaqueCommandBecomesUnknownAfterRealSIGKILL(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SIGKILL journal recovery test requires Linux")
	}
	workspace := t.TempDir()
	database := filepath.Join(t.TempDir(), "jobs.db")
	tool, err := NewHostCommandTool(workspace)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(HostCommandInput{Command: "printf 'effect\\n' >> opaque.log"})
	job := submitAndClose(t, database, tool, JobSpec{
		Name: "kill opaque command", Steps: []StepSpec{{Tool: tool.Name(), Input: input}},
	})

	runWorkerUntilSIGKILL(t, database, workspace, job.ID, FaultAfterEffect)
	marker := filepath.Join(workspace, "opaque.log")
	if content, err := os.ReadFile(marker); err != nil || string(content) != "effect\n" {
		t.Fatalf("effect before recovery = %q, err = %v", content, err)
	}
	recovery := openTestEngine(t, database, Options{
		Owner: "recovery-worker", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
	}, tool)
	waitForPersistedLeasesToExpire(t, recovery, job.ID)
	job, err = recovery.Run(t.Context(), job.ID)
	if !errors.Is(err, ErrUnknown) || job.Status != JobUnknown {
		t.Fatalf("job = %#v, err = %v", job, err)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "effect\n" {
		t.Fatalf("opaque effect was repeated: %q, err = %v", content, err)
	}
	attempts, err := recovery.Attempts(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != AttemptUnknown {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestDurableKill9Helper(t *testing.T) {
	if os.Getenv(killHelperFlag) != "1" {
		return
	}
	database := os.Getenv(killHelperDatabase)
	workspace := os.Getenv(killHelperWorkspace)
	jobID := os.Getenv(killHelperJob)
	point := FaultPoint(os.Getenv(killHelperPoint))
	editTool, err := NewFileEditTool(workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(90)
	}
	commandTool, err := NewHostCommandTool(workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}
	engine, err := Open(database, Options{
		Owner: "worker-that-will-die", LeaseTTL: 120 * time.Millisecond, HeartbeatInterval: 30 * time.Millisecond,
		FaultInjector: func(actual FaultPoint, _ FaultContext) {
			if actual == point {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
				select {}
			}
		},
	}, editTool, commandTool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(92)
	}
	defer engine.Close()
	if _, err := engine.Run(context.Background(), jobID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(93)
	}
	os.Exit(0)
}

func submitAndClose(t *testing.T, database string, tool Tool, spec JobSpec) Job {
	t.Helper()
	engine, err := Open(database, Options{Owner: "submitter"}, tool)
	if err != nil {
		t.Fatal(err)
	}
	job, submitErr := engine.Submit(t.Context(), spec)
	closeErr := engine.Close()
	if err := errors.Join(submitErr, closeErr); err != nil {
		t.Fatal(err)
	}
	return job
}

func runWorkerUntilSIGKILL(t *testing.T, database, workspace, jobID string, point FaultPoint) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDurableKill9Helper$")
	command.Env = replaceEnvironment(os.Environ(), map[string]string{
		killHelperFlag: "1", killHelperDatabase: database, killHelperWorkspace: workspace,
		killHelperJob: jobID, killHelperPoint: string(point),
	})
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("worker was not killed: err = %v, output = %s", err, output)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("worker status = %v, output = %s", exitErr.Sys(), output)
	}
}

func replaceEnvironment(current []string, replacements map[string]string) []string {
	filtered := make([]string, 0, len(current)+len(replacements))
	for _, entry := range current {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := replacements[name]; !replaced {
			filtered = append(filtered, entry)
		}
	}
	for name, value := range replacements {
		filtered = append(filtered, name+"="+value)
	}
	return filtered
}

func countEvent(events []Event, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}
