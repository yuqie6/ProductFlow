package imagesession

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestGenerateRejectsInsufficientQuota(t *testing.T) {
	ss := newSessionServer(t)
	merchantID := auth.MustDevMerchantID(t, ss.db)
	acct := loadQuotaAccount(t, ss.db, merchantID)
	if acct.AvailableUnits > 0 {
		if _, err := (&quota.Service{DB: ss.db}).Adjust(context.Background(), merchantID, "drain-"+clockid.New(), -acct.AvailableUnits, "drain", ""); err != nil {
			t.Fatal(err)
		}
	}

	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "额度不足"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	resp := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "should reject", "size": "1024x1024",
	})
	ss.mustStatus(t, resp, http.StatusConflict)
	var body map[string]any
	ss.decode(t, resp, &body)
	if body["detail"] != "可用额度不足" {
		t.Fatalf("detail=%v", body["detail"])
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	if len(session.GenerationTasks) != 0 {
		t.Fatalf("insufficient quota must not enqueue tasks, got %d", len(session.GenerationTasks))
	}
}

func TestGenerateSuccessSettlesQuotaHold(t *testing.T) {
	ss := newSessionServer(t)
	merchantID := auth.MustDevMerchantID(t, ss.db)
	before := loadQuotaAccount(t, ss.db, merchantID)

	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "额度成功"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "ok", "size": "1024x1024",
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	if len(session.GenerationTasks) != 1 {
		t.Fatalf("tasks=%d", len(session.GenerationTasks))
	}
	taskID := session.GenerationTasks[0].ID
	key := generationQuotaKey(taskID, 0)
	hold := loadQuotaHold(t, ss.db, merchantID, key)
	if hold.Status != quota.StatusReserved || hold.AmountUnits != imageSessionQuotaUnits {
		t.Fatalf("hold after generate=%+v", hold)
	}
	if countQuotaEvents(t, ss.db, merchantID, quota.EventReserve, key) != 1 {
		t.Fatal("missing reserve event")
	}
	mid := loadQuotaAccount(t, ss.db, merchantID)
	if mid.AvailableUnits != before.AvailableUnits-imageSessionQuotaUnits || mid.ReservedUnits != before.ReservedUnits+imageSessionQuotaUnits {
		t.Fatalf("after reserve before=%+v mid=%+v", before, mid)
	}

	ss.dropDispatch(t, taskID)
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	hold = loadQuotaHold(t, ss.db, merchantID, key)
	if hold.Status != quota.StatusSettled {
		t.Fatalf("hold after success=%+v", hold)
	}
	if countQuotaEvents(t, ss.db, merchantID, quota.EventSettle, key) != 1 {
		t.Fatal("missing settle event")
	}
	after := loadQuotaAccount(t, ss.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits-imageSessionQuotaUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("after settle before=%+v after=%+v", before, after)
	}
}

func TestGenerateCancelReleasesQuotaBeforeProvider(t *testing.T) {
	ss := newSessionServer(t)
	merchantID := auth.MustDevMerchantID(t, ss.db)
	before := loadQuotaAccount(t, ss.db, merchantID)

	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "额度取消"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "cancel me", "size": "1024x1024",
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	taskID := session.GenerationTasks[0].ID
	key := generationQuotaKey(taskID, 0)
	ss.dropDispatch(t, taskID)

	cancel := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generation-tasks/"+taskID+"/cancel", map[string]any{})
	ss.mustStatus(t, cancel, http.StatusOK)

	hold := loadQuotaHold(t, ss.db, merchantID, key)
	if hold.Status != quota.StatusReleased {
		t.Fatalf("hold after cancel=%+v", hold)
	}
	if countQuotaEvents(t, ss.db, merchantID, quota.EventRelease, key) != 1 {
		t.Fatal("missing release event")
	}
	after := loadQuotaAccount(t, ss.db, merchantID)
	if after.AvailableUnits != before.AvailableUnits || after.ReservedUnits != before.ReservedUnits {
		t.Fatalf("cancel must restore balance before=%+v after=%+v", before, after)
	}
}

func TestGenerateUnknownMarksQuotaPending(t *testing.T) {
	ss := newSessionServer(t)
	merchantID := auth.MustDevMerchantID(t, ss.db)

	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "额度未知"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "unknown", "size": "1024x1024",
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	taskID := session.GenerationTasks[0].ID
	key := generationQuotaKey(taskID, 0)
	ss.dropDispatch(t, taskID)

	failExec := Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{Err: errors.New("provider crashed")}}
	if err := failExec.Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	hold := loadQuotaHold(t, ss.db, merchantID, key)
	if hold.Status != quota.StatusPendingReconciliation {
		t.Fatalf("hold after unknown=%+v", hold)
	}
	if countQuotaEvents(t, ss.db, merchantID, quota.EventMarkUnknown, key) != 1 {
		t.Fatal("missing mark_unknown event")
	}
	// 禁止把 unknown 当零消费 Release
	_, _, err := (&quota.Service{DB: ss.db}).Release(context.Background(), merchantID, key)
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("release unknown want conflict, got %v", err)
	}
}
