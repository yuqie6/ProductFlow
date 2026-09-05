package imagesession

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/media"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestImageCheckpointExitHelper(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_CHECKPOINT_CHILD") != "1" {
		t.Skip("checkpoint exit helper")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("PRODUCTFLOW_CHECKPOINT_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db, err := pfdb.OpenGorm(pool)
	if err != nil {
		t.Fatal(err)
	}
	executor := Executor{DB: db, Media: media.Store{Files: storage.Local{Root: os.Getenv("PRODUCTFLOW_CHECKPOINT_ROOT")}}, Provider: MockChatProvider{}}
	err = queue.Consume(ctx, pool, os.Getenv("PRODUCTFLOW_CHECKPOINT_DISPATCH"), os.Getenv("PRODUCTFLOW_CHECKPOINT_TASK"), map[string]queue.ActorFunc{
		queue.ActorImageSession: func(ctx context.Context, id string) error {
			err := executor.Execute(ctx, id)
			if errors.Is(err, queue.ErrLater) {
				os.Exit(23)
			}
			return err
		},
	})
	t.Fatalf("did not exit at confirmed checkpoint: %v", err)
}

func TestImageCheckpointExitRecovery(t *testing.T) {
	for _, cancelTask := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelTask), func(t *testing.T) {
			pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_checkpoint_%d", time.Now().UnixNano()))
			ss := newSessionServerWithDatabase(t, pool, db)
			session, taskID := createQueuedGeneration(t, ss, map[string]any{
				"prompt": "checkpoint exit", "size": "1024x1024", "generation_count": 2,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var dispatch queue.Dispatch
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				dispatch, err = queue.StageForActor(ctx, tx, queue.ActorImageSession, taskID, 0)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			enqueued := 0
			deliver := func(id, aggregate string) error {
				if id != dispatch.ID || aggregate != taskID {
					return fmt.Errorf("unexpected envelope %s", id)
				}
				enqueued++
				return nil
			}
			if _, err := queue.RunDispatcherOnce(ctx, pool, deliver, 10); err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, binary, "-test.run=^TestImageCheckpointExitHelper$")
			child.Env = append(os.Environ(), "PRODUCTFLOW_CHECKPOINT_CHILD=1",
				"PRODUCTFLOW_CHECKPOINT_DB="+pool.Config().ConnString(), "PRODUCTFLOW_CHECKPOINT_ROOT="+ss.root,
				"PRODUCTFLOW_CHECKPOINT_TASK="+taskID, "PRODUCTFLOW_CHECKPOINT_DISPATCH="+dispatch.ID)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 23 {
				t.Fatalf("child exit: %v %s", err, output)
			}
			detail := loadSessionDetail(t, ss, session.ID)
			task := generationTaskByID(t, detail, taskID)
			if task.Status != "queued" || task.CompletedCandidates != 1 || len(detail.Rounds) != 1 || len(task.ProviderEffects) != 1 || task.ProviderEffects[0].EffectResult != "applied" {
				t.Fatalf("checkpoint task=%+v", task)
			}
			assetID := detail.Rounds[0].GeneratedAsset.ID
			downloadURL := detail.Rounds[0].GeneratedAsset.DownloadURL
			downloadHash := func() [32]byte {
				response := ss.do(t, http.MethodGet, downloadURL, nil, "")
				ss.mustStatus(t, response, http.StatusOK)
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil || len(data) == 0 {
					t.Fatalf("download bytes=%d err=%v", len(data), err)
				}
				return sha256.Sum256(data)
			}
			firstHash := downloadHash()
			var envelope schema.AsyncDispatches
			if err := db.Where("id = ?", dispatch.ID).Take(&envelope).Error; err != nil {
				t.Fatal(err)
			}
			if envelope.Status != queue.StatusPending || envelope.LeaseToken != nil {
				t.Fatalf("checkpoint envelope=%+v", envelope)
			}
			if cancelTask {
				response := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generation-tasks/"+taskID+"/cancel", map[string]any{})
				ss.mustStatus(t, response, http.StatusOK)
				response.Body.Close()
			}
			recoveryStarted := time.Now()
			for enqueued == 1 && time.Since(recoveryStarted) < 3*time.Second {
				if _, err := queue.RunDispatcherOnce(ctx, pool, deliver, 10); err != nil {
					t.Fatal(err)
				}
				if enqueued == 1 {
					time.Sleep(20 * time.Millisecond)
				}
			}
			if enqueued != 2 {
				t.Fatalf("checkpoint deliveries=%d within 3s", enqueued)
			}
			t.Logf("checkpoint ready-to-redelivery=%s without timestamp edits", time.Since(recoveryStarted))
			provider := &countingProvider{}
			executor := Executor{DB: db, Media: ss.media, Provider: provider}
			if err := queue.Consume(ctx, pool, dispatch.ID, taskID, map[string]queue.ActorFunc{queue.ActorImageSession: executor.Execute}); err != nil {
				t.Fatal(err)
			}
			detail = loadSessionDetail(t, ss, session.ID)
			task = generationTaskByID(t, detail, taskID)
			wantStatus, wantCalls, wantCount := "succeeded", 1, 2
			if cancelTask {
				wantStatus, wantCalls, wantCount = "cancelled", 0, 1
			}
			if task.Status != wantStatus || provider.calls != wantCalls || task.CompletedCandidates != wantCount || task.Attempts != 1 || len(detail.Rounds) != wantCount {
				t.Fatalf("recovered task=%+v calls=%d rounds=%d", task, provider.calls, len(detail.Rounds))
			}
			preserved := false
			for _, round := range detail.Rounds {
				preserved = preserved || round.GeneratedAsset.ID == assetID
			}
			if !preserved {
				t.Fatal("first asset lost")
			}
			if downloadHash() != firstHash {
				t.Fatal("first asset bytes changed")
			}
			if len(task.ProviderEffects) != wantCount {
				t.Fatalf("final effects=%d want=%d", len(task.ProviderEffects), wantCount)
			}
			for _, effect := range task.ProviderEffects {
				if effect.EffectResult != "applied" {
					t.Fatalf("final effect=%+v", effect)
				}
			}
			if err := db.Where("id = ?", dispatch.ID).Take(&envelope).Error; err != nil {
				t.Fatal(err)
			}
			if envelope.Status != queue.StatusConsumed || envelope.LeaseToken != nil {
				t.Fatalf("final envelope=%+v", envelope)
			}
			t.Logf("process_exit=23 cancel=%t deliveries=2 resumed_provider_calls=%d completed=%d attempts=1 first_asset_preserved=true", cancelTask, provider.calls, task.CompletedCandidates)
		})
	}
}
