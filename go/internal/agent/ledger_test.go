package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestFocusCanvasMutationReplayAndConflict(t *testing.T) {
	as := newAgentServer(t, HTTPGateway{}, "")
	session, err := as.svc.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	convID := session.Conversations[0].ConversationID
	key := clockid.New()
	first, err := as.svc.FocusCanvasTool(context.Background(), convID, key, []string{"n1"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first["accepted"] != true || first["request_id"] == nil {
		t.Fatalf("first %+v", first)
	}
	replay, err := as.svc.FocusCanvasTool(context.Background(), convID, key, []string{"n1"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if replay["request_id"] != first["request_id"] {
		t.Fatalf("replay %+v first %+v", replay, first)
	}
	_, err = as.svc.FocusCanvasTool(context.Background(), convID, key, []string{"n2"}, nil, nil)
	var conflict apperr.Error
	if !errors.As(err, &conflict) || conflict.Status != 409 {
		t.Fatalf("conflict %v", err)
	}

	applied, err := as.svc.ReconcileGraphTool(context.Background(), convID, focusCanvasTool, key, map[string]any{}, map[string]any{
		"node_ids": []string{"n1"}, "edge_ids": []string(nil), "group_ids": []string(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied.State != "applied" {
		t.Fatalf("reconcile %+v", applied)
	}
	missing, err := as.svc.ReconcileGraphTool(context.Background(), convID, focusCanvasTool, clockid.New(), map[string]any{}, map[string]any{
		"node_ids": []string{"n1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing.State != "not_applied" {
		t.Fatalf("missing %+v", missing)
	}

	empty, err := as.svc.FocusCanvasTool(context.Background(), convID, clockid.New(), nil, nil, nil)
	if empty != nil {
		t.Fatalf("empty %+v", empty)
	}
	if !errors.As(err, &conflict) || conflict.Status != 400 {
		t.Fatalf("empty err %v", err)
	}
}
