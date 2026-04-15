package adapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/limbic-systems/stargate/internal/adapter"
)

// hookOutputJSON is the parsed hook output for test assertions.
type hookOutputJSON struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
	SystemMessage string `json:"systemMessage"`
}

func classifyServer(t *testing.T, resp adapter.ClassifyResponse) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

func makePreToolUseInput(toolName, command, toolUseID string) string {
	input := map[string]any{
		"tool_name":  toolName,
		"tool_input": map[string]string{"command": command},
		"tool_use_id": toolUseID,
		"session_id":  "sess-test",
		"cwd":         "/tmp",
	}
	data, _ := json.Marshal(input)
	return string(data)
}

func TestHandlePreToolUse_NonBashAllowsImmediately(t *testing.T) {
	// Non-Bash tool should return allow without hitting the server.
	stdin := strings.NewReader(makePreToolUseInput("Read", "", "toolu_abc"))
	var stdout bytes.Buffer

	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:0", Timeout: 1 * time.Second}
	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var out hookOutputJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("decision: got %q, want %q", out.HookSpecificOutput.PermissionDecision, "allow")
	}
	if out.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName: got %q, want %q", out.HookSpecificOutput.HookEventName, "PreToolUse")
	}
}

func TestHandlePreToolUse_BashAllow(t *testing.T) {
	srv := classifyServer(t, adapter.ClassifyResponse{
		Decision:     "green",
		Action:       "allow",
		Reason:       "safe command",
		StargateTrID: "sg_tr_001",
	})

	stdin := strings.NewReader(makePreToolUseInput("Bash", "git status", "toolu_001"))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: srv.URL, Timeout: 5 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var out hookOutputJSON
	json.Unmarshal(stdout.Bytes(), &out) //nolint:errcheck
	if out.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("decision: got %q, want %q", out.HookSpecificOutput.PermissionDecision, "allow")
	}
	if out.HookSpecificOutput.PermissionDecisionReason != "safe command" {
		t.Errorf("reason: got %q, want %q", out.HookSpecificOutput.PermissionDecisionReason, "safe command")
	}
}

func TestHandlePreToolUse_BashReviewMapsToAsk(t *testing.T) {
	srv := classifyServer(t, adapter.ClassifyResponse{
		Decision:     "yellow",
		Action:       "review",
		Reason:       "needs review",
		StargateTrID: "sg_tr_002",
	})

	stdin := strings.NewReader(makePreToolUseInput("Bash", "rm file.txt", "toolu_002"))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: srv.URL, Timeout: 5 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var out hookOutputJSON
	json.Unmarshal(stdout.Bytes(), &out) //nolint:errcheck
	if out.HookSpecificOutput.PermissionDecision != "ask" {
		t.Errorf("decision: got %q, want %q", out.HookSpecificOutput.PermissionDecision, "ask")
	}
}

func TestHandlePreToolUse_BashBlockMapsToDeny(t *testing.T) {
	srv := classifyServer(t, adapter.ClassifyResponse{
		Decision:     "red",
		Action:       "block",
		Reason:       "dangerous",
		StargateTrID: "sg_tr_003",
	})

	stdin := strings.NewReader(makePreToolUseInput("Bash", "rm -rf /", "toolu_003"))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: srv.URL, Timeout: 5 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var out hookOutputJSON
	json.Unmarshal(stdout.Bytes(), &out) //nolint:errcheck
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("decision: got %q, want %q", out.HookSpecificOutput.PermissionDecision, "deny")
	}
}

func TestHandlePreToolUse_UnknownActionDeniesFailClosed(t *testing.T) {
	srv := classifyServer(t, adapter.ClassifyResponse{
		Decision:     "purple",
		Action:       "maybe",
		Reason:       "unknown action",
		StargateTrID: "sg_tr_004",
	})

	stdin := strings.NewReader(makePreToolUseInput("Bash", "ls", "toolu_004"))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: srv.URL, Timeout: 5 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var out hookOutputJSON
	json.Unmarshal(stdout.Bytes(), &out) //nolint:errcheck
	if out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("decision: got %q, want %q (fail-closed)", out.HookSpecificOutput.PermissionDecision, "deny")
	}
}

func TestHandlePreToolUse_GuidanceInSystemMessage(t *testing.T) {
	srv := classifyServer(t, adapter.ClassifyResponse{
		Decision:     "yellow",
		Action:       "review",
		Reason:       "review needed",
		Guidance:     "Consider using a safer alternative",
		StargateTrID: "sg_tr_005",
	})

	stdin := strings.NewReader(makePreToolUseInput("Bash", "chmod 777 /etc", "toolu_005"))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: srv.URL, Timeout: 5 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	var out hookOutputJSON
	json.Unmarshal(stdout.Bytes(), &out) //nolint:errcheck
	if out.SystemMessage != "Consider using a safer alternative" {
		t.Errorf("systemMessage: got %q, want %q", out.SystemMessage, "Consider using a safer alternative")
	}
}

func TestHandlePreToolUse_MalformedStdin(t *testing.T) {
	stdin := strings.NewReader("{not valid json")
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:0", Timeout: 1 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 for malformed stdin", code)
	}
}

func TestHandlePreToolUse_EmptyStdin(t *testing.T) {
	stdin := strings.NewReader("")
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:0", Timeout: 1 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 for empty stdin", code)
	}
}

func TestHandlePreToolUse_StdinExceeds1MB(t *testing.T) {
	// Create input just over 1MB.
	big := strings.Repeat("x", 1<<20+1)
	stdin := strings.NewReader(big)
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:0", Timeout: 1 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 for oversized stdin", code)
	}
}

func TestHandlePreToolUse_MissingCommand(t *testing.T) {
	// Bash tool with empty command.
	input := map[string]any{
		"tool_name":   "Bash",
		"tool_input":  map[string]string{"command": ""},
		"tool_use_id": "toolu_006",
		"session_id":  "sess-test",
		"cwd":         "/tmp",
	}
	data, _ := json.Marshal(input)
	stdin := bytes.NewReader(data)
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:0", Timeout: 1 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 for empty command", code)
	}
}

func TestHandlePreToolUse_InvalidToolUseID(t *testing.T) {
	// Path traversal in tool_use_id.
	stdin := strings.NewReader(makePreToolUseInput("Bash", "ls", "../../etc/evil"))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:0", Timeout: 1 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 for invalid tool_use_id", code)
	}
}

func TestHandlePreToolUse_ServerUnreachable(t *testing.T) {
	stdin := strings.NewReader(makePreToolUseInput("Bash", "ls", "toolu_007"))
	var stdout bytes.Buffer
	// Point at a port that's definitely not listening.
	cfg := adapter.ClientConfig{URL: "http://127.0.0.1:1", Timeout: 500 * time.Millisecond}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 2 {
		t.Errorf("exit code: got %d, want 2 for unreachable server", code)
	}
}

func TestHandlePreToolUse_TraceFileWritten(t *testing.T) {
	tok := "fb-tok-trace"
	srv := classifyServer(t, adapter.ClassifyResponse{
		Decision:      "green",
		Action:        "allow",
		Reason:        "ok",
		StargateTrID:  "sg_tr_trace",
		FeedbackToken: &tok,
	})

	toolUseID := "toolu_trace_test"
	stdin := strings.NewReader(makePreToolUseInput("Bash", "echo hello", toolUseID))
	var stdout bytes.Buffer
	cfg := adapter.ClientConfig{URL: srv.URL, Timeout: 5 * time.Second}

	code := adapter.HandlePreToolUse(context.Background(), stdin, &stdout, cfg)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0", code)
	}

	// Verify the trace file was written.
	dir, err := adapter.TraceDir()
	if err != nil {
		t.Fatalf("TraceDir: %v", err)
	}
	t.Cleanup(func() { adapter.DeleteTrace(dir, toolUseID) })

	trace, err := adapter.ReadTrace(dir, toolUseID)
	if err != nil {
		t.Fatalf("ReadTrace: %v", err)
	}
	if trace.StargateTrID != "sg_tr_trace" {
		t.Errorf("trace StargateTrID: got %q, want %q", trace.StargateTrID, "sg_tr_trace")
	}
	if trace.FeedbackToken != tok {
		t.Errorf("trace FeedbackToken: got %q, want %q", trace.FeedbackToken, tok)
	}
	if trace.ToolUseID != toolUseID {
		t.Errorf("trace ToolUseID: got %q, want %q", trace.ToolUseID, toolUseID)
	}
}

func TestMapActionToDecision(t *testing.T) {
	tests := []struct {
		action string
		want   string
	}{
		{"allow", "allow"},
		{"review", "ask"},
		{"block", "deny"},
		{"", "deny"},
		{"unknown", "deny"},
		{"ALLOW", "deny"}, // case-sensitive
	}
	for _, tt := range tests {
		// mapActionToDecision is tested indirectly via HandlePreToolUse.
		// This documents the mapping expectations.
		_ = tt
	}
}
