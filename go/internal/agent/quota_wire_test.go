package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

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
	if _, err := svc.Adjust(context.Background(), merchantID, "seed-agent-"+clockid.New(), units, "test fixture", ""); err != nil {
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

func loadQuotaHold(t *testing.T, db *gorm.DB, merchantID, key string) schema.MerchantQuotaHolds {
	t.Helper()
	var row schema.MerchantQuotaHolds
	if err := db.Where("merchant_id = ? AND idempotency_key = ?", merchantID, key).Take(&row).Error; err != nil {
		t.Fatalf("load hold %s: %v", key, err)
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

func TestModelInvocationRejectsInsufficientQuota(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	merchantID := auth.MustDevMerchantID(t, as.db)
	acct := loadQuotaAccount(t, as.db, merchantID)
	if acct.AvailableUnits > 0 {
		if _, err := (&quota.Service{DB: as.db}).Adjust(context.Background(), merchantID, "drain-"+clockid.New(), -acct.AvailableUnits, "drain", ""); err != nil {
			t.Fatal(err)
		}
	}

	claimed := claimOpenTurn(t, as)
	authHdr := http.Header{"Authorization": []string{"Bearer tok"}}
	payload := json.RawMessage(`{"model_request_id":"model:quota-reject","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`)
	cp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": payload,
	}, authHdr)
	as.mustStatus(t, cp, http.StatusConflict)
	var body map[string]any
	as.decode(t, cp, &body)
	if body["detail"] != "可用额度不足" {
		t.Fatalf("detail=%v", body["detail"])
	}
	var n int64
	if err := as.db.Model(&schema.AgentModelInvocations{}).
		Where("turn_projection_id = ? AND model_request_id = ?", claimed.projectionID, "model:quota-reject").
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("insufficient quota must not create invocation, got %d", n)
	}
	var holds int64
	if err := as.db.Model(&schema.MerchantQuotaHolds{}).
		Where("merchant_id = ? AND idempotency_key = ?", merchantID, modelInvocationQuotaKey(claimed.projectionID, "model:quota-reject")).
		Count(&holds).Error; err != nil {
		t.Fatal(err)
	}
	if holds != 0 {
		t.Fatalf("insufficient quota must not leave hold, got %d", holds)
	}
}

func TestModelInvocationSuccessSettlesQuotaHold(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	merchantID := auth.MustDevMerchantID(t, as.db)
	before := loadQuotaAccount(t, as.db, merchantID)

	claimed := claimOpenTurn(t, as)
	authHdr := http.Header{"Authorization": []string{"Bearer tok"}}
	requestID := "model:quota-ok"
	key := modelInvocationQuotaKey(claimed.projectionID, requestID)
	payload := json.RawMessage(`{"model_request_id":"` + requestID + `","provider":"openai-responses","model":"test-model","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`)
	cp := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "sequence": 1,
		"kind": "before_model_request", "payload": payload,
	}, authHdr)
	as.mustStatus(t, cp, http.StatusOK)
	cp.Body.Close()

	hold := loadQuotaHold(t, as.db, merchantID, key)
	if hold.Status != quota.StatusReserved || hold.AmountUnits != agentModelQuotaUnits {
		t.Fatalf("hold after before_model_request=%+v", hold)
	}
	if countQuotaEvents(t, as.db, merchantID, quota.EventReserve, key) != 1 {
		t.Fatal("missing reserve event")
	}
	mid := loadQuotaAccount(t, as.db, merchantID)
	if mid.AvailableUnits != before.AvailableUnits-agentModelQuotaUnits || mid.ReservedUnits != before.ReservedUnits+agentModelQuotaUnits {
		t.Fatalf("after reserve before=%+v mid=%+v", before, mid)
	}

	created := time.Now().UTC()
	responseID := "resp-" + clockid.New()
	first := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/events/batch", map[string]any{
		"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken, "events": []any{
			map[string]any{
				"sequence": 1, "schema_version": 1, "run_id": claimed.runID, "turn_id": claimed.turnID,
				"kind":       "assistant/message",
				"payload":    json.RawMessage(`{"model_request_id":"` + requestID + `","reason":"stop","duration_ms":12,"provider_response_id":"` + responseID + `","usage":{"input":2,"output":3,"total_tokens":5}}`),
				"created_at": created,
			},
		},
	}, authHdr)
	as.mustStatus(t, first, http.StatusOK)
	first.Body.Close()

	hold = loadQuotaHold(t, as.db, merchantID, key)
	if hold.Status != quota.StatusSettled {
		t.Fatalf("hold after success=%+v", hold)
	}
	if countQuotaEvents(t, as.db, merchantID, quota.EventSettle, key) != 1 {
		t.Fatal("missing settle event")
	}
	after := loadQuotaAccount(t, as.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits-agentModelQuotaUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("after settle before=%+v after=%+v", before, after)
	}
}

func TestModelInvocationUnknownMarksQuotaPending(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	merchantID := auth.MustDevMerchantID(t, as.db)
	claimed := createClaimedJournalTurn(t, as)
	requestID := "model:quota-unknown"
	key := modelInvocationQuotaKey(claimed.turn.ID, requestID)
	payload, err := json.Marshal(map[string]any{
		"model_request_id": requestID,
		"provider":         "openai-responses",
		"model":            "test-model",
		"execution_mode":   "foreground",
		"harness_hash":     testHarnessHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.AppendCheckpoint(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, 1, "before_model_request", payload); err != nil {
		t.Fatal(err)
	}
	hold := loadQuotaHold(t, as.db, merchantID, key)
	if hold.Status != quota.StatusReserved {
		t.Fatalf("hold after reserve=%+v", hold)
	}

	expireClaimedTurn(t, as, claimed)
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}

	hold = loadQuotaHold(t, as.db, merchantID, key)
	if hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("hold after unknown=%+v", hold)
	}
	if countQuotaEvents(t, as.db, merchantID, quota.EventMarkUnknown, key) != 1 {
		t.Fatal("missing mark_unknown event")
	}
	_, _, err = (&quota.Service{DB: as.db}).Release(context.Background(), merchantID, key)
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("release unknown want conflict, got %v", err)
	}
}
