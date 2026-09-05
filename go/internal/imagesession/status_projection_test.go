package imagesession

import (
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
	for i, got := range status.GenerationTasks {
		index := len(tasks) - 1 - i
		for j := range got.ProviderEffects {
			got.ProviderEffects[j].CreatedAt = got.ProviderEffects[j].CreatedAt.UTC()
			got.ProviderEffects[j].UpdatedAt = got.ProviderEffects[j].UpdatedAt.UTC()
		}
		if got.ID != tasks[index].ID || got.Status != tasks[index].Status || got.Prompt != tasks[index].Prompt ||
			!reflect.DeepEqual(got.ProviderEffects, []EffectResponse{effectFromModel(effects[index])}) {
			t.Fatalf("task/effect projection changed for %s", got.ID)
		}
	}
	t.Logf("STATUS_PROJECTION active_tasks=26 effects=26 unused_provider_bytes=%d response_bytes=%d", len(raw)*54, len(body))
	// The shared effect reader also serves bounded detail responses.
	if _, err := ss.svc.Get(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if effectReads != 2 {
		t.Fatalf("detail did not exercise shared effect projection: reads=%d", effectReads)
	}
}
