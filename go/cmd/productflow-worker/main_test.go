package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestWorkerSignalWaitsForHandler(t *testing.T) {
	redisBinary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is required for the isolated worker signal regression")
	}
	// Unix socket paths must remain short even when the checkout path is long.
	dir, err := os.MkdirTemp("", "pf-worker-signal-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	redis := exec.Command(redisBinary, "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no")
	if err := redis.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redis.Process.Kill(); _ = redis.Wait() })
	readyBy := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(readyBy) {
			t.Fatal("isolated Redis did not create its socket")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkerSignalChild$")
	child.Env = append(os.Environ(), "PRODUCTFLOW_WORKER_SIGNAL_TEST_SOCKET="+socket)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("worker exited before its handler drained: %v\n%s", err, output)
	}
}

func TestWorkerSignalChild(t *testing.T) {
	socket := os.Getenv("PRODUCTFLOW_WORKER_SIGNAL_TEST_SOCKET")
	if socket == "" {
		t.Skip("subprocess helper")
	}
	opt := asynq.RedisClientOpt{Network: "unix", Addr: socket}
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, ShutdownTimeout: 3 * time.Second})
	started, finished := make(chan struct{}), make(chan struct{})
	mux := asynq.NewServeMux()
	mux.HandleFunc("shutdown-probe", func(context.Context, *asynq.Task) error {
		close(started)
		time.Sleep(700 * time.Millisecond)
		close(finished)
		return nil
	})
	received := make(chan os.Signal, 1)
	signal.Notify(received, syscall.SIGTERM)
	defer signal.Stop(received)
	stop := make(chan os.Signal, 1)
	go func() {
		sig := <-received
		// Let any other signal receiver begin shutdown before this one resumes.
		time.Sleep(100 * time.Millisecond)
		stop <- sig
	}()
	client := asynq.NewClient(opt)
	defer client.Close()
	if _, err := client.Enqueue(asynq.NewTask("shutdown-probe", nil)); err != nil {
		t.Fatal(err)
	}
	go func() { <-started; _ = syscall.Kill(os.Getpid(), syscall.SIGTERM) }()
	if err := runWorker(server, mux, stop); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("shutdown returned while the handler was still running")
	}
}
