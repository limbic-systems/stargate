package adapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/limbic-systems/stargate/internal/adapter"
)

// newTestConfig returns a ClientConfig pointed at the given test server URL.
func newTestConfig(serverURL string) adapter.ClientConfig {
	return adapter.ClientConfig{
		URL:     serverURL,
		Timeout: 5 * time.Second,
	}
}

// --- Classify tests ---

func TestClassify_AllowResponse(t *testing.T) {
	want := adapter.ClassifyResponse{
		Decision:     "green",
		Action:       "allow",
		Reason:       "safe read-only command",
		StargateTrID: "trace-001",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/classify" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want) //nolint:errcheck
	}))
	defer srv.Close()

	got, err := adapter.Classify(context.Background(), newTestConfig(srv.URL), adapter.ClassifyRequest{
		Command: "git status",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Decision != want.Decision {
		t.Errorf("Decision: got %q, want %q", got.Decision, want.Decision)
	}
	if got.Action != want.Action {
		t.Errorf("Action: got %q, want %q", got.Action, want.Action)
	}
	if got.Reason != want.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, want.Reason)
	}
	if got.StargateTrID != want.StargateTrID {
		t.Errorf("StargateTrID: got %q, want %q", got.StargateTrID, want.StargateTrID)
	}
}

func TestClassify_DenyResponse(t *testing.T) {
	tok := "fb-tok-123"
	want := adapter.ClassifyResponse{
		Decision:      "red",
		Action:        "deny",
		Reason:        "destructive command",
		StargateTrID:  "trace-002",
		FeedbackToken: &tok,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want) //nolint:errcheck
	}))
	defer srv.Close()

	got, err := adapter.Classify(context.Background(), newTestConfig(srv.URL), adapter.ClassifyRequest{
		Command: "rm -rf /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "deny" {
		t.Errorf("Action: got %q, want \"deny\"", got.Action)
	}
	if got.FeedbackToken == nil || *got.FeedbackToken != tok {
		t.Errorf("FeedbackToken: got %v, want %q", got.FeedbackToken, tok)
	}
}

func TestClassify_Server500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := adapter.Classify(context.Background(), newTestConfig(srv.URL), adapter.ClassifyRequest{
		Command: "ls",
	})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestClassify_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json`)) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := adapter.Classify(context.Background(), newTestConfig(srv.URL), adapter.ClassifyRequest{
		Command: "ls",
	})
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestClassify_Timeout(t *testing.T) {
	// Handler blocks on a channel instead of sleeping a fixed duration.
	// The channel is closed before srv.Close() so the handler unblocks
	// immediately, keeping test execution fast.
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer srv.Close()
	defer close(done) // unblock handler before srv.Close() waits for connections

	cfg := adapter.ClientConfig{
		URL:     srv.URL,
		Timeout: 50 * time.Millisecond,
	}
	_, err := adapter.Classify(context.Background(), cfg, adapter.ClassifyRequest{Command: "ls"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestClassify_ConnectionRefusedReturnsError verifies that Classify returns
// an error when the server is not listening (ECONNREFUSED). The retry logic
// (one retry after 100ms) is exercised but not deterministically tested here
// because doPostWithRetry creates its own http.Client internally — injecting
// a custom transport would require API changes that are out of scope.
func TestClassify_ConnectionRefusedReturnsError(t *testing.T) {
	// Point at a port that's definitely not listening.
	cfg := adapter.ClientConfig{
		URL:     "http://127.0.0.1:1",
		Timeout: 500 * time.Millisecond,
	}

	_, err := adapter.Classify(context.Background(), cfg, adapter.ClassifyRequest{Command: "ls"})
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

// --- SendFeedback tests ---

func TestSendFeedback_Success(t *testing.T) {
	var gotBody FeedbackBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/feedback" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req := adapter.FeedbackRequest{
		StargateTrID:  "trace-fb-001",
		ToolUseID:     "tool-use-abc",
		FeedbackToken: "fb-tok-xyz",
		Outcome:       "correct",
	}
	err := adapter.SendFeedback(context.Background(), newTestConfig(srv.URL), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody.StargateTrID != req.StargateTrID {
		t.Errorf("StargateTrID: got %q, want %q", gotBody.StargateTrID, req.StargateTrID)
	}
	if gotBody.Outcome != req.Outcome {
		t.Errorf("Outcome: got %q, want %q", gotBody.Outcome, req.Outcome)
	}
}

func TestSendFeedback_Server500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	err := adapter.SendFeedback(context.Background(), newTestConfig(srv.URL), adapter.FeedbackRequest{
		StargateTrID:  "trace-err",
		FeedbackToken: "fb-tok",
		Outcome:       "correct",
	})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// FeedbackBody mirrors FeedbackRequest for decoding in tests.
type FeedbackBody struct {
	StargateTrID  string `json:"stargate_trace_id"`
	ToolUseID     string `json:"tool_use_id"`
	FeedbackToken string `json:"feedback_token"`
	Outcome       string `json:"outcome"`
}

// --- ValidateURL tests ---

func TestValidateURL_Loopback127(t *testing.T) {
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:9099"}
	if err := cfg.ValidateURL(); err != nil {
		t.Errorf("unexpected error for 127.0.0.1: %v", err)
	}
}

func TestValidateURL_LoopbackIPv6(t *testing.T) {
	cfg := adapter.ClientConfig{URL: "http://[::1]:9099"}
	if err := cfg.ValidateURL(); err != nil {
		t.Errorf("unexpected error for [::1]: %v", err)
	}
}

func TestValidateURL_PrivateIP(t *testing.T) {
	cfg := adapter.ClientConfig{URL: "http://192.168.1.1:9099"}
	if err := cfg.ValidateURL(); err == nil {
		t.Error("expected error for 192.168.1.1, got nil")
	}
}

func TestValidateURL_PublicDomain(t *testing.T) {
	cfg := adapter.ClientConfig{URL: "http://evil.com:9099"}
	if err := cfg.ValidateURL(); err == nil {
		t.Error("expected error for evil.com, got nil")
	}
}

func TestValidateURL_LocalhostRejected(t *testing.T) {
	cfg := adapter.ClientConfig{URL: "http://localhost:9099"}
	if err := cfg.ValidateURL(); err == nil {
		t.Error("expected error for localhost (DNS resolution risk), got nil")
	}
}

func TestValidateURL_AllowRemote(t *testing.T) {
	cfg := adapter.ClientConfig{
		URL:         "http://10.0.0.1:9099",
		AllowRemote: true,
	}
	if err := cfg.ValidateURL(); err != nil {
		t.Errorf("unexpected error with AllowRemote=true: %v", err)
	}
}

func TestIsServerUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"random error", errors.New("something broke"), false},
		{"connection refused", syscall.ECONNREFUSED, true},
		{"wrapped connection refused", &url.Error{Op: "Post", Err: &net.OpError{Err: syscall.ECONNREFUSED}}, true},
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"url timeout", &url.Error{Op: "Post", Err: &timeoutErr{}}, true},
		{"dns error", &net.DNSError{Err: "no such host"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adapter.IsServerUnavailable(tt.err); got != tt.want {
				t.Errorf("IsServerUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type timeoutErr struct{}

func (e *timeoutErr) Error() string   { return "timeout" }
func (e *timeoutErr) Timeout() bool   { return true }
func (e *timeoutErr) Temporary() bool { return true }

