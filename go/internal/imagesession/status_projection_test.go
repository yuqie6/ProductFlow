package imagesession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

func TestImageSessionStatusSkipsRawProviderPayloads(t *testing.T) {
	ss := newSessionServer(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	session := schema.ImageSessions{ID: clockid.New(), Title: "status projection", CreatedAt: now, UpdatedAt: now}
	if err := ss.db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	roundIDs := ss.seedGeneratedRounds(t, session.ID, 1, now)
	group := clockid.New()
	raw := `{"unused":"` + strings.Repeat("x", 64*1024) + `"}`
	if err := ss.db.Model(&schema.ImageSessionRounds{}).Where("id = ?", roundIDs[0]).Updates(map[string]any{
		"generation_group_id": group, "provider_request_json": raw, "provider_output_json": raw,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var tasks []schema.ImageSessionGenerationTasks
	var effects []schema.ImageSessionProviderEffects
	responseID, providerStatus := "fixture-response", "in_progress"
	for i := 0; i < 26; i++ {
		task := schema.ImageSessionGenerationTasks{
			ID: clockid.New(), SessionID: session.ID, Status: "queued", Prompt: fmt.Sprintf("task %d", i),
			Size: "1024x1024", GenerationCount: 1, CreatedAt: now.Add(time.Duration(i) * time.Second),
		}
		if i == 0 {
			task.Status = "running"
			attempt := clockid.New()
			task.ActiveAttemptID, task.StartedAt = &attempt, &now
		}
		tasks = append(tasks, task)
		effects = append(effects, schema.ImageSessionProviderEffects{
			ID: clockid.New(), GenerationTaskID: task.ID, CandidateStartIndex: 1, CandidateCount: 1,
			OperationKey: clockid.New(), EffectKind: "image_session_generation", RequestHash: strings.Repeat("a", 64),
			ProviderName: "fixture", AttemptID: clockid.New(), EffectResult: "unknown", ReconciliationState: "not_requested",
			ProviderResponseID: &responseID, ProviderStatus: &providerStatus,
			RequestJSON: &raw, ResultJSON: &raw, Detail: &task.Prompt, CreatedAt: now, UpdatedAt: now,
		})
	}
	t.Cleanup(func() {
		if err := ss.db.Where("generation_task_id IN (?)", ss.db.Model(&schema.ImageSessionGenerationTasks{}).Select("id").Where("session_id = ?", session.ID)).Delete(&schema.ImageSessionProviderEffects{}).Error; err != nil {
			t.Error(err)
		}
		if err := ss.db.Where("session_id = ?", session.ID).Delete(&schema.ImageSessionGenerationTasks{}).Error; err != nil {
			t.Error(err)
		}
	})
	if err := ss.db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := ss.db.Create(&effects).Error; err != nil {
		t.Fatal(err)
	}

	// Inspect actual query destinations: a small HTTP response alone does not prove a narrow PG read.
	latestReads, effectReads := 0, 0
	callback := "test:status_provider_projection"
	if err := ss.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
		switch rows := db.Statement.Dest.(type) {
		case *schema.ImageSessionRounds:
			latestReads++
			if rows.ProviderRequestJSON != nil || rows.ProviderOutputJSON != nil || rows.Prompt != "" {
				t.Error("latest-round status query loaded content instead of identity only")
			}
		case *[]schema.ImageSessionProviderEffects:
			effectReads++
			for _, row := range *rows {
				if row.RequestJSON != nil || row.ResultJSON != nil {
					t.Error("effect response query loaded raw provider payloads")
					break
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.db.Callback().Query().Remove(callback) })
	status, err := ss.svc.Status(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latestReads != 1 || effectReads != 1 {
		t.Fatalf("latest reads=%d effect reads=%d", latestReads, effectReads)
	}
	if status.RoundsCount != 1 || status.LatestRoundID == nil || *status.LatestRoundID != roundIDs[0] ||
		status.LatestGenerationGroupID == nil || *status.LatestGenerationGroupID != group ||
		!status.HasActiveGenerationTask || len(status.GenerationTasks) != len(tasks) {
		t.Fatal("status lost latest round identity or active tasks")
	}
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(`"prompt":`)) {
		t.Fatal("status repeated task prompts on the wire")
	}
	for i, got := range status.GenerationTasks {
		index := len(tasks) - 1 - i
		for j := range got.ProviderEffects {
			got.ProviderEffects[j].CreatedAt = got.ProviderEffects[j].CreatedAt.UTC()
			got.ProviderEffects[j].UpdatedAt = got.ProviderEffects[j].UpdatedAt.UTC()
		}
		if got.ID != tasks[index].ID || got.Status != tasks[index].Status || got.Prompt != "" ||
			!reflect.DeepEqual(got.ProviderEffects, []EffectResponse{effectFromModel(effects[index])}) {
			t.Fatalf("task/effect projection changed for %s", got.ID)
		}
	}
	t.Logf("STATUS_PROJECTION active_tasks=26 effects=26 unused_provider_bytes=%d response_bytes=%d", len(raw)*54, len(body))
	detail, err := ss.svc.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if effectReads != 2 {
		t.Fatalf("detail did not exercise shared effect projection: reads=%d", effectReads)
	}
	if len(detail.GenerationTasks) == 0 || detail.GenerationTasks[0].Prompt == "" {
		t.Fatal("detail lost active-task prompts")
	}
}

func TestImageSessionQueueOverviewOnlyForReturnedTasks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		detail bool
		status string
		want   int
	}{
		{"empty_status", false, "", 0},
		{"empty_detail", true, "", 0},
		{"terminal_status", false, "cancelled", 0},
		{"terminal_detail", true, "cancelled", 1},
		{"active_status", false, "queued", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss := newSessionServer(t)
			now := time.Now().UTC()
			session := schema.ImageSessions{ID: clockid.New(), Title: tc.name, CreatedAt: now, UpdatedAt: now}
			if err := ss.db.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			if tc.status != "" {
				task := schema.ImageSessionGenerationTasks{
					ID: clockid.New(), SessionID: session.ID, Status: tc.status,
					Prompt: "queue projection", Size: "1024x1024", GenerationCount: 1, CreatedAt: now,
				}
				t.Cleanup(func() {
					if err := ss.db.Where("id = ?", task.ID).Delete(&schema.ImageSessionGenerationTasks{}).Error; err != nil {
						t.Error(err)
					}
				})
				if err := ss.db.Create(&task).Error; err != nil {
					t.Fatal(err)
				}
			}
			queueReads := 0
			callback := "test:empty_task_queue_reads"
			if err := ss.db.Callback().Query().After("gorm:query").Register(callback, func(db *gorm.DB) {
				if isImageSessionQueueOverviewQuery(db) {
					queueReads++
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ss.db.Callback().Query().Remove(callback) })
			var tasks []TaskResponse
			if tc.detail {
				detail, err := ss.svc.Get(context.Background(), session.ID)
				if err != nil {
					t.Fatal(err)
				}
				tasks = detail.GenerationTasks
			} else {
				status, err := ss.svc.Status(context.Background(), session.ID)
				if err != nil {
					t.Fatal(err)
				}
				tasks = status.GenerationTasks
			}
			t.Logf("QUEUE_PROJECTION route=%s tasks=%d queue_queries=%d", tc.name, len(tasks), queueReads)
			if tasks == nil || len(tasks) != tc.want {
				t.Fatalf("tasks=%v want count=%d and non-null array", tasks, tc.want)
			}
			if tc.want == 0 && queueReads != 0 {
				t.Error("empty task projection read an unused global queue overview")
			}
			if tc.want > 0 && (queueReads != 2 || tasks[0].QueueMaxConcurrentTasks != 20) {
				t.Error("returned task lost its queue overview")
			}
		})
	}
}

func isImageSessionQueueOverviewQuery(db *gorm.DB) bool {
	if db.DryRun {
		return false
	}
	if strings.Contains(db.Statement.SQL.String(), "AS session_counts") {
		return true
	}
	switch db.Statement.Dest.(type) {
	case *schema.AppSettings:
		return db.Statement.Table == "app_settings"
	case *int64:
		return db.Statement.Table == "image_session_generation_tasks" || db.Statement.Table == "workflow_graph_runs"
	default:
		return false
	}
}

func TestImageSessionStatusActiveFlagMatchesTaskSnapshot(t *testing.T) {
	for _, enqueue := range []bool{true, false} {
		name := "complete_before_task_read"
		if enqueue {
			name = "enqueue_before_task_read"
		}
		t.Run(name, func(t *testing.T) {
			ss := newSessionServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			now := time.Now().UTC()
			session := schema.ImageSessions{ID: clockid.New(), Title: name, CreatedAt: now, UpdatedAt: now}
			if err := ss.db.WithContext(ctx).Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			task := schema.ImageSessionGenerationTasks{
				ID: clockid.New(), SessionID: session.ID, Status: "queued", Prompt: "snapshot",
				Size: "1024x1024", GenerationCount: 1, CreatedAt: now,
			}
			t.Cleanup(func() {
				if err := ss.db.Where("id = ?", task.ID).Delete(&schema.ImageSessionGenerationTasks{}).Error; err != nil {
					t.Error(err)
				}
			})
			if !enqueue {
				if err := ss.db.WithContext(ctx).Create(&task).Error; err != nil {
					t.Fatal(err)
				}
			}
			countReads, taskReads, countsBeforeList := 0, 0, 0
			callback := "test:active_task_snapshot"
			// Commit from another DB connection between the old COUNT and the task SELECT.
			if err := ss.db.Callback().Query().Before("gorm:query").Register(callback, func(db *gorm.DB) {
				if db.Statement.Table != "image_session_generation_tasks" {
					return
				}
				switch db.Statement.Dest.(type) {
				case *int64:
					countReads++
					if taskReads == 0 {
						countsBeforeList++
					}
				case *[]schema.ImageSessionGenerationTasks:
					taskReads++
					if taskReads != 1 {
						return
					}
					var err error
					if enqueue {
						err = ss.db.WithContext(ctx).Create(&task).Error
					} else {
						err = ss.db.WithContext(ctx).Model(&schema.ImageSessionGenerationTasks{}).
							Where("id = ?", task.ID).Update("status", "cancelled").Error
					}
					if err != nil {
						db.AddError(err)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ss.db.Callback().Query().Remove(callback) })
			status, err := ss.svc.Status(ctx, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("ACTIVE_SNAPSHOT enqueue=%t task_count_queries=%d counts_before_list=%d task_list_queries=%d has_active=%t tasks=%d",
				enqueue, countReads, countsBeforeList, taskReads, status.HasActiveGenerationTask, len(status.GenerationTasks))
			wantTasks := 0
			if enqueue {
				wantTasks = 1
			}
			if taskReads != 1 || len(status.GenerationTasks) != wantTasks || status.HasActiveGenerationTask != enqueue {
				t.Error("activity flag and task list must describe the same task read")
			}
			if countsBeforeList != 0 {
				t.Error("status made a redundant, independently visible active-task COUNT")
			}
		})
	}
}
