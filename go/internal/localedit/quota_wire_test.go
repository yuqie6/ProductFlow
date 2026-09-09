package localedit

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
)

func submitLocalEditTask(t *testing.T, es *editServer, productID, sourceAssetID, idempotencyKey string) TaskResponse {
	t.Helper()
	form := createForm(t, sourceAssetID, false)
	draft := es.do(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits", form.body, form.contentType)
	es.mustStatus(t, draft, http.StatusCreated)
	var task TaskResponse
	es.decode(t, draft, &task)
	submitted := es.doJSON(t, http.MethodPost, "/api/v3/products/"+productID+"/image-edits/"+task.ID+"/submit", map[string]any{
		"idempotency_key": idempotencyKey,
	})
	es.mustStatus(t, submitted, http.StatusAccepted)
	es.decode(t, submitted, &task)
	return task
}

func TestLocalEditRejectsInsufficientQuota(t *testing.T) {
	provider := MockProvider{Cap: SupportedCapability("mock-local")}
	es := newEditServer(t, provider)
	merchantID := auth.MustDevMerchantID(t, es.db)
	acct := loadQuotaAccount(t, es.db, merchantID)
	if acct.AvailableUnits > 0 {
		if _, err := (&quota.Service{DB: es.db}).Adjust(context.Background(), merchantID, "drain-"+clockid.New(), -acct.AvailableUnits, "drain", ""); err != nil {
			t.Fatal(err)
		}
	}

	created := es.createProduct(t)
	task := submitLocalEditTask(t, es, created.Product.ID, created.CreatedAssets[0].ID, "quota-insufficient")
	es.executeLocally(t, task.ID, Executor{DB: es.db, Media: es.media, Provider: provider})

	got := es.do(t, http.MethodGet, "/api/v3/products/"+created.Product.ID+"/image-edits/"+task.ID, nil, "")
	es.mustStatus(t, got, http.StatusOK)
	es.decode(t, got, &task)
	if task.Status != "failed" || task.FailureReason == nil || *task.FailureReason != "可用额度不足" {
		t.Fatalf("want failed 可用额度不足, got %+v", task)
	}
	if !task.IsRetryable {
		t.Fatal("insufficient quota should stay retryable after top-up")
	}
	var holdCount int64
	if err := es.db.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key LIKE ?", merchantID, "local-edit:"+task.ID+":%").
		Count(&holdCount).Error; err != nil {
		t.Fatal(err)
	}
	if holdCount != 0 {
		t.Fatalf("insufficient quota must not leave holds, got %d", holdCount)
	}
}

func TestLocalEditSuccessSettlesQuotaHold(t *testing.T) {
	provider := MockProvider{Cap: SupportedCapability("mock-local")}
	es := newEditServer(t, provider)
	merchantID := auth.MustDevMerchantID(t, es.db)
	before := loadQuotaAccount(t, es.db, merchantID)

	created := es.createProduct(t)
	task := submitLocalEditTask(t, es, created.Product.ID, created.CreatedAssets[0].ID, "quota-settle")
	es.executeLocally(t, task.ID, Executor{DB: es.db, Media: es.media, Provider: provider})

	got := es.do(t, http.MethodGet, "/api/v3/products/"+created.Product.ID+"/image-edits/"+task.ID, nil, "")
	es.mustStatus(t, got, http.StatusOK)
	es.decode(t, got, &task)
	if task.Status != "succeeded" {
		t.Fatalf("status %s", task.Status)
	}
	key := findAnyLocalEditHoldKey(t, es.db, merchantID)
	hold := loadQuotaHold(t, es.db, merchantID, key)
	if hold.Status != quota.StatusSettled || hold.AmountUnits != localEditQuotaUnits {
		t.Fatalf("hold after success=%+v", hold)
	}
	if countQuotaEvents(t, es.db, merchantID, quota.EventReserve, key) != 1 {
		t.Fatal("missing reserve event")
	}
	if countQuotaEvents(t, es.db, merchantID, quota.EventSettle, key) != 1 {
		t.Fatal("missing settle event")
	}
	after := loadQuotaAccount(t, es.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits-localEditQuotaUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("after settle before=%+v after=%+v", before, after)
	}
}

func TestLocalEditUnknownMarksQuotaPending(t *testing.T) {
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	merchantID := auth.MustDevMerchantID(t, es.db)

	created := es.createProduct(t)
	task := submitLocalEditTask(t, es, created.Product.ID, created.CreatedAssets[0].ID, "quota-unknown")
	failExec := Executor{
		DB: es.db, Media: es.media,
		Provider: MockProvider{Cap: SupportedCapability("mock-local"), Err: errors.New("provider crashed")},
	}
	es.executeLocally(t, task.ID, failExec)

	got := es.do(t, http.MethodGet, "/api/v3/products/"+created.Product.ID+"/image-edits/"+task.ID, nil, "")
	es.mustStatus(t, got, http.StatusOK)
	es.decode(t, got, &task)
	if task.Status != "unknown" {
		t.Fatalf("status %s", task.Status)
	}
	key := findAnyLocalEditHoldKey(t, es.db, merchantID)
	hold := loadQuotaHold(t, es.db, merchantID, key)
	if hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("hold after unknown=%+v", hold)
	}
	if countQuotaEvents(t, es.db, merchantID, quota.EventMarkUnknown, key) != 1 {
		t.Fatal("missing mark_unknown event")
	}
	_, _, err := (&quota.Service{DB: es.db}).Release(context.Background(), merchantID, key)
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("release unknown want conflict, got %v", err)
	}
}

func TestLocalEditCancelReleasesQuotaBeforeProvider(t *testing.T) {
	provider := MockProvider{Cap: SupportedCapability("mock-local")}
	es := newEditServer(t, provider)
	merchantID := auth.MustDevMerchantID(t, es.db)
	before := loadQuotaAccount(t, es.db, merchantID)

	created := es.createProduct(t)
	task := submitLocalEditTask(t, es, created.Product.ID, created.CreatedAssets[0].ID, "quota-cancel-release")

	// 模拟 Execute 已 Reserve 但尚未进入 provider 边界（progress_phase 仍 claimed）。
	attemptID := clockid.New()
	key := editQuotaKey(task.ID, attemptID)
	if _, _, err := (&quota.Service{DB: es.db}).Reserve(context.Background(), merchantID, key, localEditQuotaUnits, quota.DefaultPriceVersionID); err != nil {
		t.Fatal(err)
	}
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := es.pool.Exec(context.Background(), `
		INSERT INTO local_image_edit_provider_attempts (
			id, task_id, attempt_id, attempt_number, operation_key, request_hash,
			phase, effect_result, provider_name, created_at, updated_at
		) VALUES ($1, $2, $3, 1, $4, $5, 'claimed', 'pending', 'pending', NOW(), NOW())
	`, clockid.New(), task.ID, attemptID, "local-image-edit:"+task.ID, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := es.pool.Exec(context.Background(), `
		UPDATE local_image_edit_tasks SET
			status = 'running', active_attempt_id = $2, progress_phase = 'claimed',
			started_at = NOW(), finished_at = NULL, updated_at = NOW(),
			attempts = GREATEST(attempts, 1), is_retryable = TRUE
		WHERE id = $1
	`, task.ID, attemptID); err != nil {
		t.Fatal(err)
	}
	es.dropRiverJob(t, task.ID)

	cancel := es.doJSON(t, http.MethodPost, "/api/v3/products/"+created.Product.ID+"/image-edits/"+task.ID+"/cancel", map[string]any{})
	es.mustStatus(t, cancel, http.StatusOK)

	hold := loadQuotaHold(t, es.db, merchantID, key)
	if hold.Status != quota.StatusReleased {
		t.Fatalf("hold after cancel=%+v", hold)
	}
	if countQuotaEvents(t, es.db, merchantID, quota.EventRelease, key) != 1 {
		t.Fatal("missing release event")
	}
	after := loadQuotaAccount(t, es.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("cancel must restore balance before=%+v after=%+v", before, after)
	}
}

func TestLateAttemptCannotFinalizeNewAttemptQuota(t *testing.T) {
	for _, action := range []string{"release", "settle", "unknown"} {
		t.Run(action, func(t *testing.T) {
			es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
			ctx := context.Background()
			merchantID := auth.MustDevMerchantID(t, es.db)
			taskID, oldAttempt, newAttempt := clockid.New(), clockid.New(), clockid.New()
			oldKey, newKey := editQuotaKey(taskID, oldAttempt), editQuotaKey(taskID, newAttempt)
			q := &quota.Service{DB: es.db}
			if _, _, err := q.Reserve(ctx, merchantID, oldKey, localEditQuotaUnits, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := q.Release(ctx, merchantID, oldKey); err != nil {
				t.Fatal(err)
			}
			if _, _, err := q.Reserve(ctx, merchantID, newKey, localEditQuotaUnits, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			before := loadQuotaAccount(t, es.db, merchantID)
			e := Executor{DB: es.db}
			var actionErr error
			switch action {
			case "release":
				actionErr = e.releaseEditQuota(ctx, merchantID, taskID, oldAttempt)
			case "settle":
				actionErr = e.settleEditQuota(ctx, merchantID, taskID, oldAttempt)
			case "unknown":
				actionErr = e.markEditQuotaUnknown(ctx, merchantID, taskID, oldAttempt)
			}
			if action == "release" && actionErr != nil {
				t.Fatal(actionErr)
			}
			if action != "release" {
				var ae apperr.Error
				if !errors.As(actionErr, &ae) || ae.Status != http.StatusConflict {
					t.Fatalf("expected old released hold conflict, got %v", actionErr)
				}
			}
			hold := loadQuotaHold(t, es.db, merchantID, newKey)
			if hold.Status != quota.StatusReserved {
				t.Fatalf("late %s changed new attempt hold: %+v", action, hold)
			}
			after := loadQuotaAccount(t, es.db, merchantID)
			if after.AvailableUnits != before.AvailableUnits || after.ReservedUnits != before.ReservedUnits {
				t.Fatalf("late %s changed account: before=%+v after=%+v", action, before, after)
			}
			for _, event := range []string{quota.EventRelease, quota.EventSettle, quota.EventMarkUnknown} {
				if countQuotaEvents(t, es.db, merchantID, event, newKey) != 0 {
					t.Fatalf("late %s wrote new attempt event %s", action, event)
				}
			}
		})
	}
}

func TestCancelQuotaFailureRollsBackTaskAndAttempt(t *testing.T) {
	for _, phase := range []string{"claimed", "provider_call"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
			created := es.createProduct(t)
			taskID := createQueuedLocalEdit(t, es, created, "cancel-quota-rollback")
			markStaleLocalEdit(t, es, taskID, phase)
			var attemptID string
			if err := es.pool.QueryRow(ctx, "SELECT active_attempt_id FROM local_image_edit_tasks WHERE id=$1", taskID).Scan(&attemptID); err != nil {
				t.Fatal(err)
			}
			merchantID := auth.MustDevMerchantID(t, es.db)
			key := editQuotaKey(taskID, attemptID)
			if _, _, err := (&quota.Service{DB: es.db}).Reserve(ctx, merchantID, key, localEditQuotaUnits, quota.DefaultPriceVersionID); err != nil {
				t.Fatal(err)
			}
			before := loadQuotaAccount(t, es.db, merchantID)
			constraint := "test_cancel_quota_failure"
			if _, err := es.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key <> '"+key+"' OR status = 'reserved')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := es.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			owned := auth.WithMerchantID(ctx, merchantID)
			if _, err := es.svc.Cancel(owned, created.Product.ID, taskID, nil); err == nil {
				t.Fatal("cancel must report quota persistence failure")
			}
			var status, active, effect string
			if err := es.pool.QueryRow(ctx, "SELECT t.status,t.active_attempt_id,a.effect_result FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id WHERE t.id=$1", taskID).Scan(&status, &active, &effect); err != nil {
				t.Fatal(err)
			}
			if status != "running" || active != attemptID || effect != "pending" {
				t.Fatalf("partial cancellation: status=%s active=%s effect=%s", status, active, effect)
			}
			after := loadQuotaAccount(t, es.db, merchantID)
			if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
				t.Fatalf("quota balance did not roll back: before=%+v after=%+v", before, after)
			}
			if _, err := es.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, err := es.svc.Cancel(owned, created.Product.ID, taskID, nil); err != nil {
					t.Fatal(err)
				}
			}
			hold := loadQuotaHold(t, es.db, merchantID, key)
			want := quota.StatusReleased
			if phase == "provider_call" {
				want = quota.StatusPendingReconciliation
			}
			if hold.Status != want {
				t.Fatalf("retry cancellation hold=%s want=%s", hold.Status, want)
			}
		})
	}
}

func TestTerminalQuotaFailureRemainsRecoverable(t *testing.T) {
	for _, outcome := range []string{"success", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			provider := &capturingEditProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local")}}
			if outcome == "unknown" {
				provider.Err = errors.New("provider timeout")
			}
			es := newEditServer(t, provider)
			created := es.createProduct(t)
			taskID := createQueuedLocalEdit(t, es, created, "terminal-quota-failure")
			const constraint = "test_terminal_quota_failure"
			if _, err := es.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key NOT LIKE 'local-edit:"+taskID+":%' OR status = 'reserved')"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := es.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
					t.Error(err)
				}
			})
			executor := Executor{DB: es.db, Media: es.media, Provider: provider}
			if err := executor.Execute(ctx, taskID); err == nil {
				t.Fatal("terminal quota persistence failure must propagate")
			}
			if provider.lastSize == "" {
				t.Fatal("provider was not called")
			}
			var status, phase, holdStatus string
			var resultID *string
			if err := es.pool.QueryRow(ctx, "SELECT t.status,t.result_asset_id,a.phase,h.status FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id JOIN merchant_quota_holds h ON h.idempotency_key='local-edit:'||t.id||':'||a.attempt_id WHERE t.id=$1", taskID).Scan(&status, &resultID, &phase, &holdStatus); err != nil {
				t.Fatal(err)
			}
			if status != "running" || resultID != nil || phase != "provider_call" || holdStatus != quota.StatusReserved {
				t.Fatalf("partial terminal: status=%s result=%v phase=%s hold=%s", status, resultID, phase, holdStatus)
			}
			var assets int
			if err := es.pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE product_id=$1 AND id<>$2", created.Product.ID, created.CreatedAssets[0].ID).Scan(&assets); err != nil {
				t.Fatal(err)
			}
			if assets != 0 {
				t.Fatalf("rolled back success leaked %d assets", assets)
			}
			// Recovery must also remain atomic while the same quota failure persists.
			future := time.Now().UTC().Add(2 * time.Hour)
			if _, err := recoverLocalEditState(ctx, es.db, taskID, time.Minute, future); err == nil {
				t.Fatal("recovery must report quota write failure")
			}
			if err := es.pool.QueryRow(ctx, "SELECT status FROM local_image_edit_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "running" {
				t.Fatalf("failed recovery committed %s", status)
			}
			if _, err := es.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
				t.Fatal(err)
			}
			if recovered, err := recoverLocalEditState(ctx, es.db, taskID, time.Minute, future); err != nil || recovered != "unknown" {
				t.Fatalf("recovery=%s err=%v", recovered, err)
			}
			provider.lastSize = ""
			if err := executor.Execute(ctx, taskID); err != nil {
				t.Fatal(err)
			}
			if provider.lastSize != "" {
				t.Fatal("recovered provider effect was repeated")
			}
			if err := es.pool.QueryRow(ctx, "SELECT t.status,h.status FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id JOIN merchant_quota_holds h ON h.idempotency_key='local-edit:'||t.id||':'||a.attempt_id WHERE t.id=$1", taskID).Scan(&status, &holdStatus); err != nil {
				t.Fatal(err)
			}
			if status != "unknown" || holdStatus != quota.StatusPendingReconciliation {
				t.Fatalf("status=%s hold=%s", status, holdStatus)
			}
		})
	}
}

func TestProviderPreparationCannotReserveAfterCancellation(t *testing.T) {
	ctx := context.Background()
	es := newEditServer(t, MockProvider{Cap: SupportedCapability("mock-local")})
	created := es.createProduct(t)
	taskID := createQueuedLocalEdit(t, es, created, "cancel-before-reserve")
	executor := Executor{DB: es.db, Media: es.media}
	claimed, attemptID, err := executor.claim(ctx, taskID)
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	merchantID := auth.MustDevMerchantID(t, es.db)
	if _, err := es.svc.Cancel(auth.WithMerchantID(ctx, merchantID), created.Product.ID, taskID, nil); err != nil {
		t.Fatal(err)
	}
	if err := executor.prepareProviderCall(ctx, merchantID, taskID, attemptID, "mock-local", nil); !errors.Is(err, errAttemptFenced) {
		t.Fatalf("want fence, got %v", err)
	}
	var count int64
	if err := es.db.Model(&schema.MerchantQuotaHolds{}).Where("idempotency_key = ?", editQuotaKey(taskID, attemptID)).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("cancelled attempt created quota hold")
	}
}

func TestProviderPreparationQuotaFailureRollsBackBoundary(t *testing.T) {
	ctx := context.Background()
	provider := &capturingEditProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local")}}
	es := newEditServer(t, provider)
	taskID := createQueuedLocalEdit(t, es, es.createProduct(t), "reserve-write-failure")
	const constraint = "test_prepare_quota_failure"
	if _, err := es.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds ADD CONSTRAINT "+constraint+" CHECK (idempotency_key NOT LIKE 'local-edit:"+taskID+":%')"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := es.pool.Exec(context.Background(), "ALTER TABLE merchant_quota_holds DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	executor := Executor{DB: es.db, Media: es.media, Provider: provider}
	var pgErr *pgconn.PgError
	if err := executor.Execute(ctx, taskID); !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
		t.Fatalf("quota database cause must propagate: %v", err)
	}
	if provider.lastSize != "" {
		t.Fatal("provider called without quota reservation")
	}
	var status, phase, attemptPhase string
	if err := es.pool.QueryRow(ctx, "SELECT t.status,t.progress_phase,a.phase FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id WHERE t.id=$1", taskID).Scan(&status, &phase, &attemptPhase); err != nil {
		t.Fatal(err)
	}
	if status != "running" || phase != "claimed" || attemptPhase != "claimed" {
		t.Fatalf("partial provider boundary: task=%s phase=%s attempt=%s", status, phase, attemptPhase)
	}

	var oldAttempt string
	if err := es.pool.QueryRow(ctx, "SELECT active_attempt_id FROM local_image_edit_tasks WHERE id=$1", taskID).Scan(&oldAttempt); err != nil {
		t.Fatal(err)
	}
	var holds int64
	if err := es.db.Model(&schema.MerchantQuotaHolds{}).Where("idempotency_key = ?", editQuotaKey(taskID, oldAttempt)).Count(&holds).Error; err != nil {
		t.Fatal(err)
	}
	if holds != 0 {
		t.Fatalf("rolled back preparation left %d holds", holds)
	}
	if outcome, err := recoverLocalEditState(ctx, es.db, taskID, time.Minute, time.Now().UTC().Add(2*time.Minute)); err != nil || outcome != "requeued" {
		t.Fatalf("recovery=%s err=%v", outcome, err)
	}
	var effect string
	if err := es.pool.QueryRow(ctx, "SELECT effect_result FROM local_image_edit_provider_attempts WHERE task_id=$1 AND attempt_id=$2", taskID, oldAttempt).Scan(&effect); err != nil {
		t.Fatal(err)
	}
	if effect != "failed" {
		t.Fatalf("old effect=%s", effect)
	}
	if _, err := es.pool.Exec(ctx, "ALTER TABLE merchant_quota_holds DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(ctx, taskID); err != nil {
		t.Fatal(err)
	}
	if provider.lastSize == "" {
		t.Fatal("recovered attempt did not call provider")
	}
	if err := es.pool.QueryRow(ctx, "SELECT status FROM local_image_edit_tasks WHERE id=$1", taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" {
		t.Fatalf("recovered status=%s", status)
	}
	var newAttempt, holdStatus string
	if err := es.pool.QueryRow(ctx, `SELECT a.attempt_id,h.status FROM local_image_edit_provider_attempts a JOIN merchant_quota_holds h ON h.idempotency_key='local-edit:'||a.task_id||':'||a.attempt_id WHERE a.task_id=$1 AND a.attempt_id<>$2`, taskID, oldAttempt).Scan(&newAttempt, &holdStatus); err != nil {
		t.Fatal(err)
	}
	if newAttempt == oldAttempt || holdStatus != quota.StatusSettled {
		t.Fatalf("new attempt=%s hold=%s", newAttempt, holdStatus)
	}
}

func TestSuccessRequiresCurrentAttemptReservation(t *testing.T) {
	ctx := context.Background()
	provider := &capturingEditProvider{MockProvider: MockProvider{Cap: SupportedCapability("mock-local")}}
	es := newEditServer(t, provider)
	created := es.createProduct(t)
	taskID := createQueuedLocalEdit(t, es, created, "missing-success-hold")
	executor := Executor{DB: es.db, Media: es.media, Provider: provider}
	claimed, attempt, err := executor.claim(ctx, taskID)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	snap, err := executor.loadSnapshot(ctx, taskID, attempt)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := es.pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE product_id=$1", created.Product.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	result := EditResult{Bytes: snap.SourceBytes, MIME: snap.SourceMIME}
	if err := executor.persistResult(ctx, snap, attempt, result); !apperr.IsNotFound(err) {
		t.Fatalf("missing reservation accepted: %v", err)
	}
	var status, effect string
	var asset *string
	var after int
	if err := es.pool.QueryRow(ctx, "SELECT t.status,t.result_asset_id,a.effect_result FROM local_image_edit_tasks t JOIN local_image_edit_provider_attempts a ON a.task_id=t.id AND a.attempt_id=$2 WHERE t.id=$1", taskID, attempt).Scan(&status, &asset, &effect); err != nil {
		t.Fatal(err)
	}
	if err := es.pool.QueryRow(ctx, "SELECT count(*) FROM product_image_assets WHERE product_id=$1", created.Product.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if status != "running" || asset != nil || effect != "pending" || after != before {
		t.Fatalf("partial success status=%s asset=%v effect=%s assets=%d/%d", status, asset, effect, after, before)
	}
	merchantID := auth.MustDevMerchantID(t, es.db)
	if err := executor.reserveEditQuota(ctx, merchantID, taskID, attempt); err != nil {
		t.Fatal(err)
	}
	if err := executor.persistResult(ctx, snap, attempt, result); err != nil {
		t.Fatal(err)
	}
	if hold := loadQuotaHold(t, es.db, merchantID, editQuotaKey(taskID, attempt)); hold.Status != quota.StatusSettled {
		t.Fatalf("hold=%s", hold.Status)
	}
}
