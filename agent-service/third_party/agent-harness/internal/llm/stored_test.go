package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
)

func TestStoredResponseCreateThenRetrieve(t *testing.T) {
	var created capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "POST /responses":
			if got := request.Header.Get("X-Client-Request-Id"); got != "attempt-123" {
				t.Errorf("X-Client-Request-Id = %q", got)
			}
			if err := json.NewDecoder(request.Body).Decode(&created); err != nil {
				t.Errorf("decode create: %v", err)
			}
			_, _ = writer.Write([]byte(`{"id":"resp_stored","status":"queued","output":[]}`))
		case "GET /responses/resp_stored":
			_, _ = writer.Write([]byte(`{"id":"resp_stored","status":"completed","usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}},"output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	client := New("key", server.URL, "model")
	client.HTTP = server.Client()
	var authorized string
	var recorded Usage
	client.BeforeCreate = func(_ context.Context, requestKey string) error {
		authorized = requestKey
		return nil
	}
	client.RecordUsage = func(_ context.Context, usage Usage) error {
		recorded = usage
		return nil
	}
	user := "work"
	handle, err := client.CreateStored(t.Context(), []Message{{Role: "user", Content: &user}}, nil, "attempt-123")
	if err != nil || handle.ID != "resp_stored" || handle.Status != "queued" {
		t.Fatalf("handle = %#v, err = %v", handle, err)
	}
	if created.Store == nil || !*created.Store || created.Background == nil || !*created.Background {
		t.Fatalf("stored create request = %#v", created)
	}
	if authorized != "attempt-123" {
		t.Fatalf("authorized request key = %q", authorized)
	}
	result, err := client.RetrieveStored(t.Context(), handle.ID)
	if err != nil || !result.Complete || result.Message.String() != "done" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if recorded.ResponseID != "resp_stored" || recorded.InputTokens != 12 || recorded.OutputTokens != 5 ||
		recorded.TotalTokens != 17 || recorded.CachedTokens != 3 || recorded.ReasoningTokens != 2 {
		t.Fatalf("recorded usage = %#v", recorded)
	}
}

func TestCreateBudgetHookRejectsBeforeNetworkIO(t *testing.T) {
	var requests int
	client := New("key", "https://provider.invalid", "model")
	client.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("must not run")
	})}
	client.BeforeCreate = func(context.Context, string) error { return errors.New("budget exhausted") }
	user := "work"
	if _, err := client.CreateStored(t.Context(), []Message{{Role: "user", Content: &user}}, nil, "attempt-123"); err == nil || !strings.Contains(err.Error(), "预算拒绝") {
		t.Fatalf("create error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("network requests = %d", requests)
	}
}

func TestStoredTerminalResponseRequiresProviderUsageWhenAccountingEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_missing_usage","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	t.Cleanup(server.Close)
	client := New("key", server.URL, "model")
	client.HTTP = server.Client()
	client.RecordUsage = func(context.Context, Usage) error { return nil }
	if _, err := client.RetrieveStored(t.Context(), "resp_missing_usage"); err == nil || !IsStoredTerminal(err) || !strings.Contains(err.Error(), "provider usage") {
		t.Fatalf("retrieve error = %v", err)
	}
}

func TestStoredCreateClassifiesTransportAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name      string
		wrote     bool
		ambiguous bool
	}{
		{name: "dial failure is definite", ambiguous: false},
		{name: "failure after headers is ambiguous", wrote: true, ambiguous: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := New("key", "https://provider.invalid", "model")
			client.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if test.wrote {
					trace := httptrace.ContextClientTrace(request.Context())
					trace.WroteHeaders()
				}
				return nil, errors.New("connection lost")
			})}
			user := "work"
			_, err := client.CreateStored(context.Background(), []Message{{Role: "user", Content: &user}}, nil, "attempt-123")
			if err == nil || IsAmbiguousRequest(err) != test.ambiguous {
				t.Fatalf("err = %v, ambiguous = %v", err, IsAmbiguousRequest(err))
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
