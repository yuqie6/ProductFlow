package product

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/quota"
	"gorm.io/gorm"
)

func mustSeedMerchantQuota(t *testing.T, db *gorm.DB, merchantID string, units int64) {
	t.Helper()
	if units <= 0 {
		return
	}
	svc := &quota.Service{DB: db}
	if _, err := svc.Adjust(context.Background(), merchantID, "seed-product-"+clockid.New(), units, "test fixture", ""); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
}

func loadQuotaAccount(t *testing.T, db *gorm.DB, merchantID string) quota.Account {
	t.Helper()
	acct, err := (&quota.Service{DB: db}).GetAccount(context.Background(), merchantID)
	if err != nil {
		t.Fatal(err)
	}
	return acct
}

func loadLatestSourceNoteHold(t *testing.T, db *gorm.DB, merchantID string) schema.MerchantQuotaHolds {
	t.Helper()
	var row schema.MerchantQuotaHolds
	err := db.Where("merchant_id = ? AND idempotency_key LIKE ?", merchantID, "product-source-note:%").
		Order("created_at DESC").
		Take(&row).Error
	if err != nil {
		t.Fatalf("load source-note hold: %v", err)
	}
	return row
}

func countQuotaEvents(t *testing.T, db *gorm.DB, merchantID, eventType, key string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", merchantID, eventType, key).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func drainMerchantQuota(t *testing.T, db *gorm.DB, merchantID string) {
	t.Helper()
	acct := loadQuotaAccount(t, db, merchantID)
	if acct.AvailableUnits <= 0 {
		return
	}
	if _, err := (&quota.Service{DB: db}).Adjust(context.Background(), merchantID, "drain-"+clockid.New(), -acct.AvailableUnits, "drain", ""); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateSourceNoteRejectsInsufficientQuota(t *testing.T) {
	stub := &stubSourceNote{payload: map[string]any{"visible": "不应调用"}}
	ps := newProductServerWith(t, Service{SourceNote: stub})
	merchantID := auth.MustDevMerchantID(t, ps.db)
	drainMerchantQuota(t, ps.db, merchantID)
	holdsBefore := countSourceNoteHolds(t, ps.db, merchantID)

	body, contentType := multipartPNGs(t, map[string]string{"product_name": "密封瓶"}, 1)
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", body, contentType)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["detail"] != "可用额度不足" {
		t.Fatalf("detail=%v", got["detail"])
	}
	if stub.req.NodeTitle != "" || len(stub.req.References) != 0 {
		t.Fatalf("insufficient quota must not call provider, got %+v", stub.req)
	}
	holdsAfter := countSourceNoteHolds(t, ps.db, merchantID)
	if holdsAfter != holdsBefore {
		t.Fatalf("insufficient quota must not create hold, before=%d after=%d", holdsBefore, holdsAfter)
	}
}

func countSourceNoteHolds(t *testing.T, db *gorm.DB, merchantID string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key LIKE ?", merchantID, "product-source-note:%").
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGenerateSourceNoteSuccessSettlesQuotaHold(t *testing.T) {
	stub := &stubSourceNote{payload: map[string]any{
		"visible": "厚壁玻璃密封瓶",
		"fields":  []any{map[string]any{"label": "材质", "value": "玻璃"}},
	}}
	ps := newProductServerWith(t, Service{SourceNote: stub})
	merchantID := auth.MustDevMerchantID(t, ps.db)
	mustSeedMerchantQuota(t, ps.db, merchantID, 10)
	before := loadQuotaAccount(t, ps.db, merchantID)

	body, contentType := multipartPNGs(t, map[string]string{"product_name": "密封瓶"}, 1)
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", body, contentType)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}

	hold := loadLatestSourceNoteHold(t, ps.db, merchantID)
	if hold.Status != quota.StatusSettled || hold.AmountUnits != sourceNoteQuotaUnits {
		t.Fatalf("hold after success=%+v", hold)
	}
	if countQuotaEvents(t, ps.db, merchantID, quota.EventReserve, hold.IdempotencyKey) != 1 {
		t.Fatal("missing reserve event")
	}
	if countQuotaEvents(t, ps.db, merchantID, quota.EventSettle, hold.IdempotencyKey) != 1 {
		t.Fatal("missing settle event")
	}
	after := loadQuotaAccount(t, ps.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits-sourceNoteQuotaUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("after settle before=%+v after=%+v", before, after)
	}
}

func TestGenerateSourceNoteProviderErrorMarksQuotaUnknown(t *testing.T) {
	stub := &stubSourceNote{err: errors.New("provider crashed")}
	ps := newProductServerWith(t, Service{SourceNote: stub})
	merchantID := auth.MustDevMerchantID(t, ps.db)
	mustSeedMerchantQuota(t, ps.db, merchantID, 10)

	body, contentType := multipartPNGs(t, map[string]string{"product_name": "密封瓶"}, 1)
	resp := ps.do(t, http.MethodPost, "/api/v2/product-source-notes/generate", body, contentType)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", resp.StatusCode, raw)
	}

	hold := loadLatestSourceNoteHold(t, ps.db, merchantID)
	if hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("hold after unknown=%+v", hold)
	}
	if countQuotaEvents(t, ps.db, merchantID, quota.EventMarkUnknown, hold.IdempotencyKey) != 1 {
		t.Fatal("missing mark_unknown event")
	}
	_, _, err := (&quota.Service{DB: ps.db}).Release(context.Background(), merchantID, hold.IdempotencyKey)
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("release unknown want conflict, got %v", err)
	}
}

func TestSourceNoteQuotaReleaseRestoresBalance(t *testing.T) {
	// 覆盖未发出路径的 Release 接线（HTTP 在 provider 错误时走 MarkUnknown）。
	ps := newProductServer(t)
	merchantID := auth.MustDevMerchantID(t, ps.db)
	mustSeedMerchantQuota(t, ps.db, merchantID, 10)
	before := loadQuotaAccount(t, ps.db, merchantID)
	requestID := clockid.New()
	ctx := context.Background()
	if err := ps.svc.reserveSourceNoteQuota(ctx, merchantID, requestID); err != nil {
		t.Fatal(err)
	}
	if err := ps.svc.releaseSourceNoteQuota(ctx, merchantID, requestID); err != nil {
		t.Fatal(err)
	}
	key := sourceNoteQuotaKey(requestID)
	hold := loadQuotaHoldByKey(t, ps.db, merchantID, key)
	if hold.Status != quota.StatusReleased {
		t.Fatalf("hold after release=%+v", hold)
	}
	if countQuotaEvents(t, ps.db, merchantID, quota.EventRelease, key) != 1 {
		t.Fatal("missing release event")
	}
	after := loadQuotaAccount(t, ps.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("release must restore balance before=%+v after=%+v", before, after)
	}
}

func loadQuotaHoldByKey(t *testing.T, db *gorm.DB, merchantID, key string) schema.MerchantQuotaHolds {
	t.Helper()
	var row schema.MerchantQuotaHolds
	if err := db.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&row).Error; err != nil {
		t.Fatalf("load hold %s: %v", key, err)
	}
	return row
}
