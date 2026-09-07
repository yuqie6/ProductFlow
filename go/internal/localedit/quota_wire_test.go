package localedit

import (
	"context"
	"errors"
	"net/http"
	"testing"

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
	es.dropDispatch(t, task.ID)

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
