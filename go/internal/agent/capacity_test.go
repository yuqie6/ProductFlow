package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfmetrics "github.com/yuqie6/productflow/internal/platform/metrics"
)

const (
	agentJournalCapacityEnv             = "PRODUCTFLOW_RUN_AGENT_JOURNAL_CAPACITY"
	agentJournalCapacityBatchSize       = 64
	agentJournalCapacityEventCount      = 10_000
	agentJournalCapacityConcurrentTurns = 25
	agentJournalCapacityEventsPerTurn   = 128
	agentJournalCapacityP95Limit        = 300 * time.Millisecond
)

func TestTurnEventSequenceCapacity(t *testing.T) {
	_, err := (Service{}).AppendEvents(context.Background(), "conversation", "execution", "owner", "lease", []EventAppendInput{{
		Sequence: maxEventSequence + 1, SchemaVersion: 1, RunID: "run", TurnID: "turn",
		Kind: "turn/start", Payload: []byte(`{}`),
	}})
	if err == nil {
		t.Fatal("expected event sequence capacity error")
	}
}

func TestAgentSSEConnectionCapacity(t *testing.T) {
	previous := pfmetrics.AgentSSEConnections.Load()
	pfmetrics.AgentSSEConnections.Store(maxSSEConnections)
	defer pfmetrics.AgentSSEConnections.Store(previous)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/events", nil)
	(Service{}).StreamTurnEvents(ctx, nil, "conversation", "projection", 0, "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestSessionTurnCapacity(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var created SessionResponse
	as.decode(t, session, &created)
	conversationID := created.Conversations[0].ConversationID
	rowPrefix := clockid.New()[:20]

	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, idempotency_key, request_hash, input_text,
			input_asset_ids_json, status, resume_required, tool_steps_json, created_at, updated_at
		)
		SELECT
			$3 || '-' || lpad(value::text, 4, '0'), $1,
			'capacity-key-' || value::text, repeat('a', 64), 'capacity',
			'[]'::jsonb, 'succeeded', FALSE, '[]'::jsonb, NOW(), NOW()
		FROM generate_series(1, $2) AS value
	`, conversationID, maxTurnsPerSession, rowPrefix); err != nil {
		t.Fatal(err)
	}

	response := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+conversationID+"/turns", map[string]any{
		"input_text": "超过容量", "idempotency_key": clockid.New(),
	})
	as.mustStatus(t, response, http.StatusConflict)
}

func TestLastFiftyTurnsQueryP95(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var created SessionResponse
	as.decode(t, session, &created)
	conversationID := created.Conversations[0].ConversationID
	rowPrefix := clockid.New()[:20]
	if _, err := as.pool.Exec(context.Background(), `
		INSERT INTO agent_turn_projections (
			id, conversation_id, idempotency_key, request_hash, input_text,
			input_asset_ids_json, status, resume_required, tool_steps_json, created_at, updated_at
		)
		SELECT
			$3 || '-' || lpad(value::text, 4, '0'), $1,
			'last50-key-' || value::text, repeat('b', 64), 'capacity',
			'[]'::jsonb, 'succeeded', FALSE, '[]'::jsonb,
			NOW() - (value || ' seconds')::interval, NOW()
		FROM generate_series(1, $2) AS value
	`, conversationID, maxTurnsPerSession, rowPrefix); err != nil {
		t.Fatal(err)
	}
	samples := make([]time.Duration, 0, 40)
	for range 40 {
		started := time.Now()
		page, err := as.svc.ListTurns(context.Background(), nil, conversationID, "", "", 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 50 {
			t.Fatalf("last page size %d", len(page.Items))
		}
		samples = append(samples, time.Since(started))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[(len(samples)*95+99)/100-1]
	if p95 > 500*time.Millisecond {
		t.Fatalf("last 50 Turn query P95 %s exceeds 500ms", p95)
	}

	seen := map[string]struct{}{}
	after := ""
	for {
		page, err := as.svc.ListTurns(context.Background(), nil, conversationID, "", after, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if _, dup := seen[item.ID]; dup {
				t.Fatalf("duplicate turn %s in 1000-turn cursor read", item.ID)
			}
			seen[item.ID] = struct{}{}
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		after = *page.NextCursor
	}
	if len(seen) != maxTurnsPerSession {
		t.Fatalf("1000-turn session cursor read %d want %d", len(seen), maxTurnsPerSession)
	}
}

func TestAgentSSETimeToFirstEventP95(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "")
	claimed := createClaimedJournalTurn(t, as)
	if _, err := as.svc.AppendEvents(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, []EventAppendInput{
		capacityJournalEvent(claimed, 1, 2),
	}); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v2/agent-conversations/%s/turns/%s/events", claimed.conversationID, claimed.turn.ID)
	samples := make([]time.Duration, 0, 20)
	for range 20 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		started := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, as.srv.URL+path, nil)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		for _, cookie := range as.cookies {
			req.AddCookie(cookie)
		}
		resp, err := as.client.Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			t.Fatalf("sse %d %s", resp.StatusCode, raw)
		}
		frame, err := readCapacitySSEFrame(resp.Body)
		elapsed := time.Since(started)
		resp.Body.Close()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(frame, "id: 1\n") || !strings.Contains(frame, "event: turn.started\n") {
			t.Fatalf("first SSE frame %q", frame)
		}
		samples = append(samples, elapsed)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[(len(samples)*95+99)/100-1]
	t.Logf("Agent SSE time-to-first-event P95=%s n=%d", p95, len(samples))
	if p95 > time.Second {
		t.Fatalf("SSE P95 %s exceeds 1s", p95)
	}
}

func TestAgentJournalCapacityGate(t *testing.T) {
	if os.Getenv(agentJournalCapacityEnv) != "1" {
		t.Skipf("set %s=1 to run the PostgreSQL Agent journal capacity gate", agentJournalCapacityEnv)
	}

	// The standard capacity dimensions are independent: one 10k-event Turn
	// proves journal depth, while 25 smaller Turns prove concurrent writers.
	// Running 25x10k would test a different, substantially larger workload.
	as := newAgentServer(t, mockGateway{}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	latencies := &appendLatencyRecorder{}
	deepLatencies := &appendLatencyRecorder{}

	deepTurn := createClaimedJournalTurn(t, as)
	if err := appendCapacityJournalTurn(ctx, as, deepTurn, agentJournalCapacityEventCount, deepLatencies); err != nil {
		t.Fatalf("append 10k-event Turn: %v", err)
	}
	if err := assertCapacityJournalTurn(ctx, as, deepTurn, agentJournalCapacityEventCount); err != nil {
		t.Fatalf("verify 10k-event Turn: %v", err)
	}

	turns := make([]claimedJournalTurn, 0, agentJournalCapacityConcurrentTurns)
	for range agentJournalCapacityConcurrentTurns {
		turns = append(turns, createClaimedJournalTurn(t, as))
	}
	results := make(chan error, len(turns))
	var writers sync.WaitGroup
	for index := range turns {
		claimed := turns[index]
		writers.Add(1)
		go func() {
			defer writers.Done()
			if err := appendCapacityJournalTurn(ctx, as, claimed, agentJournalCapacityEventsPerTurn, latencies); err != nil {
				results <- fmt.Errorf("Turn %s: %w", claimed.turn.ID, err)
			}
		}()
	}
	writers.Wait()
	close(results)
	for err := range results {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	for _, claimed := range turns {
		if err := assertCapacityJournalTurn(ctx, as, claimed, agentJournalCapacityEventsPerTurn); err != nil {
			t.Errorf("verify concurrent Turn %s: %v", claimed.turn.ID, err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	samples := latencies.snapshot()
	p95 := durationPercentile(samples, 95)
	deepSamples := deepLatencies.snapshot()
	deepP95 := durationPercentile(deepSamples, 95)
	t.Logf(
		"Agent journal capacity: deep_events=%d concurrent_turns=%d concurrent_events_per_turn=%d concurrent_batches=%d concurrent_p50=%s concurrent_p95=%s deep_batches=%d deep_p50=%s deep_p95=%s",
		agentJournalCapacityEventCount,
		agentJournalCapacityConcurrentTurns,
		agentJournalCapacityEventsPerTurn,
		len(samples),
		durationPercentile(samples, 50),
		p95,
		len(deepSamples),
		durationPercentile(deepSamples, 50),
		deepP95,
	)
	if p95 > agentJournalCapacityP95Limit {
		t.Errorf("concurrent AppendEvents batch P95=%s, want <=%s", p95, agentJournalCapacityP95Limit)
	}
	if deepP95 > agentJournalCapacityP95Limit {
		t.Errorf("deep AppendEvents batch P95=%s, want <=%s", deepP95, agentJournalCapacityP95Limit)
	}
}

func TestAgentSSEHTTPConnectionCapacityGate(t *testing.T) {
	if os.Getenv(agentJournalCapacityEnv) != "1" {
		t.Skipf("set %s=1 to run the HTTP Agent SSE capacity gate", agentJournalCapacityEnv)
	}

	as := newAgentServer(t, mockGateway{}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	claimed := createClaimedJournalTurn(t, as)
	if _, err := as.svc.AppendEvents(
		ctx,
		claimed.conversationID,
		claimed.lease.ExecutionID,
		"worker-1",
		claimed.lease.LeaseToken,
		[]EventAppendInput{capacityJournalEvent(claimed, 1, 2)},
	); err != nil {
		t.Fatalf("persist initial SSE event: %v", err)
	}

	baseline := pfmetrics.AgentSSEConnections.Load()
	if baseline != 0 {
		t.Fatalf("Agent SSE connection baseline=%d, want 0 before capacity gate", baseline)
	}
	path := fmt.Sprintf(
		"/api/v2/agent-conversations/%s/turns/%s/events",
		claimed.conversationID,
		claimed.turn.ID,
	)
	type openResult struct {
		index    int
		response *http.Response
		cancel   context.CancelFunc
		err      error
		reader   *bufio.Reader
	}
	opened := make(chan openResult, maxSSEConnections)
	for index := range maxSSEConnections {
		requestCtx, requestCancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, as.srv.URL+path, nil)
		if err != nil {
			requestCancel()
			t.Fatalf("build SSE request %d: %v", index, err)
		}
		for _, cookie := range as.cookies {
			req.AddCookie(cookie)
		}
		go func() {
			response, requestErr := as.client.Do(req)
			opened <- openResult{index: index, response: response, cancel: requestCancel, err: requestErr}
		}()
	}

	streams := make([]openResult, 0, maxSSEConnections)
	closeStreams := func() {
		for _, stream := range streams {
			stream.cancel()
			if stream.response != nil {
				_ = stream.response.Body.Close()
			}
		}
	}
	defer closeStreams()
	for range maxSSEConnections {
		select {
		case result := <-opened:
			if result.err != nil {
				result.cancel()
				closeStreams()
				t.Fatalf("open SSE stream %d: %v", result.index, result.err)
			}
			result.reader = bufio.NewReader(result.response.Body)
			streams = append(streams, result)
		case <-ctx.Done():
			closeStreams()
			t.Fatalf("open 100 SSE streams: %v", ctx.Err())
		}
	}
	for _, stream := range streams {
		if stream.response.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(stream.response.Body)
			closeStreams()
			t.Fatalf("SSE stream %d status=%d body=%s", stream.index, stream.response.StatusCode, raw)
		}
		if !strings.Contains(stream.response.Header.Get("Content-Type"), "text/event-stream") {
			closeStreams()
			t.Fatalf("SSE stream %d content-type=%q", stream.index, stream.response.Header.Get("Content-Type"))
		}
	}

	frames := make(chan error, len(streams))
	for _, stream := range streams {
		stream := stream
		go func() {
			frame, err := readCapacitySSEFrame(stream.reader)
			if err != nil {
				frames <- fmt.Errorf("SSE stream %d read persisted event: %w", stream.index, err)
				return
			}
			if !strings.Contains(frame, "id: 1\n") || !strings.Contains(frame, "event: turn.started\n") {
				frames <- fmt.Errorf("SSE stream %d first frame=%q, want persisted turn.started sequence 1", stream.index, frame)
				return
			}
			frames <- nil
		}()
	}
	for range streams {
		select {
		case err := <-frames:
			if err != nil {
				closeStreams()
				t.Fatal(err)
			}
		case <-ctx.Done():
			closeStreams()
			t.Fatalf("read persisted events from 100 SSE streams: %v", ctx.Err())
		}
	}
	if err := waitForAgentSSEConnections(ctx, int64(maxSSEConnections)); err != nil {
		closeStreams()
		t.Fatal(err)
	}

	overflowReq, err := http.NewRequestWithContext(ctx, http.MethodGet, as.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range as.cookies {
		overflowReq.AddCookie(cookie)
	}
	overflow, err := as.client.Do(overflowReq)
	if err != nil {
		t.Fatalf("open 101st SSE stream: %v", err)
	}
	overflowBody, readErr := io.ReadAll(overflow.Body)
	_ = overflow.Body.Close()
	if readErr != nil {
		t.Fatalf("read 101st SSE response: %v", readErr)
	}
	if overflow.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("101st SSE status=%d body=%s, want %d", overflow.StatusCode, overflowBody, http.StatusServiceUnavailable)
	}

	started := time.Now()
	if _, err := as.svc.AppendEvents(ctx, claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken,
		[]EventAppendInput{capacityJournalEvent(claimed, 2, 3)}); err != nil {
		t.Fatal(err)
	}
	liveLatencies := &appendLatencyRecorder{}
	for _, stream := range streams {
		go func() {
			for {
				frame, err := readCapacitySSEFrame(stream.reader)
				if err != nil {
					frames <- fmt.Errorf("live stream %d: %w", stream.index, err)
					return
				}
				if strings.HasPrefix(frame, ":") {
					continue
				}
				if !strings.Contains(frame, "id: 2\n") || !strings.Contains(frame, "event: item.delta\n") {
					frames <- fmt.Errorf("live stream %d unexpected frame %q", stream.index, frame)
					return
				}
				var event struct {
					Sequence int `json:"sequence"`
					Payload  struct {
						Delta string `json:"delta"`
					} `json:"payload"`
				}
				for _, line := range strings.Split(frame, "\n") {
					if data, ok := strings.CutPrefix(line, "data: "); ok {
						if err := json.Unmarshal([]byte(data), &event); err != nil {
							frames <- fmt.Errorf("live stream %d invalid JSON: %w", stream.index, err)
							return
						}
					}
				}
				if event.Sequence != 2 || event.Payload.Delta != "x" {
					frames <- fmt.Errorf("live stream %d wrong projection %+v", stream.index, event)
					return
				}
				liveLatencies.observe(time.Since(started))
				frames <- nil
				return
			}
		}()
	}
	for range streams {
		select {
		case err := <-frames:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatalf("live fanout: %v", ctx.Err())
		}
	}
	samples := liveLatencies.snapshot()
	p95 := durationPercentile(samples, 95)
	t.Logf("Agent SSE live fanout: streams=%d events=1 append_to_frame_p50=%s append_to_frame_p95=%s", len(samples), durationPercentile(samples, 50), p95)
	if p95 > time.Second {
		t.Errorf("live SSE P95=%s exceeds 1s", p95)
	}

	closeStreams()
	if err := waitForAgentSSEConnections(ctx, baseline); err != nil {
		t.Fatal(err)
	}
	t.Logf("Agent HTTP SSE capacity passed: active=%d overflow_status=%d baseline_after_cancel=%d", maxSSEConnections, overflow.StatusCode, baseline)
}

func readCapacitySSEFrame(body io.Reader) (string, error) {
	reader := bufio.NewReader(body)
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		frame.WriteString(line)
		if line == "\n" {
			return frame.String(), nil
		}
		if err != nil {
			return frame.String(), err
		}
	}
}

func waitForAgentSSEConnections(ctx context.Context, want int64) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		got := pfmetrics.AgentSSEConnections.Load()
		if got == want {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Agent SSE connections=%d, want %d before timeout: %w", got, want, ctx.Err())
		case <-ticker.C:
		}
	}
}

type appendLatencyRecorder struct {
	mu      sync.Mutex
	samples []time.Duration
}

func (r *appendLatencyRecorder) observe(elapsed time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, elapsed)
}

func (r *appendLatencyRecorder) snapshot() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.samples...)
}

func appendCapacityJournalTurn(
	ctx context.Context,
	as *agentServer,
	claimed claimedJournalTurn,
	eventCount int,
	latencies *appendLatencyRecorder,
) error {
	if eventCount < 2 {
		return fmt.Errorf("event count %d must include turn/start and turn/end", eventCount)
	}
	lease, err := as.svc.HeartbeatExecution(
		ctx,
		claimed.conversationID,
		claimed.lease.ExecutionID,
		"worker-1",
		claimed.lease.LeaseToken,
		"model",
	)
	if err != nil {
		return fmt.Errorf("start lease heartbeat: %w", err)
	}
	for first := 1; first <= eventCount; first += agentJournalCapacityBatchSize {
		if time.Until(lease.LeaseExpiresAt) < 20*time.Second {
			lease, err = as.svc.HeartbeatExecution(
				ctx,
				claimed.conversationID,
				claimed.lease.ExecutionID,
				"worker-1",
				claimed.lease.LeaseToken,
				"model",
			)
			if err != nil {
				return fmt.Errorf("renew lease before sequence %d: %w", first, err)
			}
		}
		last := min(first+agentJournalCapacityBatchSize-1, eventCount)
		batch := make([]EventAppendInput, 0, last-first+1)
		for sequence := first; sequence <= last; sequence++ {
			batch = append(batch, capacityJournalEvent(claimed, sequence, eventCount))
		}
		started := time.Now()
		receipts, appendErr := as.svc.AppendEvents(
			ctx,
			claimed.conversationID,
			claimed.lease.ExecutionID,
			"worker-1",
			claimed.lease.LeaseToken,
			batch,
		)
		latencies.observe(time.Since(started))
		if appendErr != nil {
			return fmt.Errorf("append sequences %d..%d: %w", first, last, appendErr)
		}
		if len(receipts) != len(batch) {
			return fmt.Errorf("sequences %d..%d returned %d receipts, want %d", first, last, len(receipts), len(batch))
		}
		for index, receipt := range receipts {
			if want := first + index; receipt.Sequence != want {
				return fmt.Errorf("receipt %d sequence=%d, want %d", index, receipt.Sequence, want)
			}
		}
	}
	if _, err := as.svc.ReleaseExecution(
		ctx,
		claimed.conversationID,
		claimed.lease.ExecutionID,
		"worker-1",
		claimed.lease.LeaseToken,
		"terminal",
	); err != nil {
		return fmt.Errorf("release terminal execution: %w", err)
	}
	return nil
}

func capacityJournalEvent(claimed claimedJournalTurn, sequence, eventCount int) EventAppendInput {
	kind := "text.chunk"
	payload := json.RawMessage(`{"delta":"x","attempt_id":"capacity-attempt","step_id":"capacity-step","content_index":0}`)
	switch sequence {
	case 1:
		kind = "turn/start"
		payload = json.RawMessage(`{"status":"running","attempt_id":"capacity-attempt"}`)
	case eventCount:
		kind = "turn/end"
		payload = json.RawMessage(`{"status":"succeeded","reason":"completed"}`)
	}
	return EventAppendInput{
		Sequence: sequence, SchemaVersion: 1,
		RunID: claimed.turn.HarnessRunID, TurnID: *claimed.turn.HarnessTurnID,
		Kind: kind, Payload: payload, CreatedAt: time.Now().UTC(),
	}
}

func assertCapacityJournalTurn(ctx context.Context, as *agentServer, claimed claimedJournalTurn, eventCount int) error {
	var count, minimum, maximum, sequenceSum int64
	if err := as.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(MIN(sequence), 0), COALESCE(MAX(sequence), 0), COALESCE(SUM(sequence), 0)
		FROM agent_turn_events
		WHERE turn_projection_id = $1
	`, claimed.turn.ID).Scan(&count, &minimum, &maximum, &sequenceSum); err != nil {
		return fmt.Errorf("query event sequence: %w", err)
	}
	wantCount := int64(eventCount)
	wantSum := wantCount * (wantCount + 1) / 2
	if count != wantCount || minimum != 1 || maximum != wantCount || sequenceSum != wantSum {
		return fmt.Errorf(
			"sequence set count=%d min=%d max=%d sum=%d, want count=%d min=1 max=%d sum=%d",
			count, minimum, maximum, sequenceSum, wantCount, wantCount, wantSum,
		)
	}

	var status, phase string
	var ownerReleased, tokenReleased, releaseRecorded bool
	if err := as.pool.QueryRow(ctx, `
		SELECT p.status, e.phase, e.owner_id IS NULL, e.lease_token IS NULL, e.released_at IS NOT NULL
		FROM agent_turn_projections p
		JOIN agent_turn_executions e ON e.turn_projection_id = p.id
		WHERE p.id = $1
	`, claimed.turn.ID).Scan(&status, &phase, &ownerReleased, &tokenReleased, &releaseRecorded); err != nil {
		return fmt.Errorf("query terminal state: %w", err)
	}
	if status != "succeeded" || phase != "terminal" || !ownerReleased || !tokenReleased || !releaseRecorded {
		return fmt.Errorf(
			"terminal state status=%s phase=%s owner_released=%t token_released=%t release_recorded=%t",
			status, phase, ownerReleased, tokenReleased, releaseRecorded,
		)
	}
	return nil
}

func durationPercentile(samples []time.Duration, percentile int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := (percentile*len(sorted) + 99) / 100
	return sorted[rank-1]
}
