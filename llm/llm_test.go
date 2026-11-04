package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatSendsToolsAndParsesToolCalls(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{
			"message": {
				"role": "assistant",
				"content": "",
				"thinking": "I should query the database",
				"tool_calls": [{"function": {"name": "query_lab_db", "arguments": {"sql": "SELECT 1"}}}]
			},
			"prompt_eval_count": 120,
			"eval_count": 15,
			"total_duration": 2000000000
		}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "qwen3.5:9b", "nomic-embed-text", false, 8192)
	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "query_lab_db", Description: "d", Parameters: map[string]any{"type": "object"}}}}

	resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools)
	if err != nil {
		t.Fatal(err)
	}

	if got.Model != "qwen3.5:9b" || got.Stream || got.Think || len(got.Tools) != 1 {
		t.Errorf("unexpected request: %+v", got)
	}
	if got.Options["num_ctx"] != float64(8192) {
		t.Errorf("num_ctx not sent: %v", got.Options)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Function.Name != "query_lab_db" {
		t.Fatalf("tool call not parsed: %+v", resp.Message)
	}
	if string(resp.Message.ToolCalls[0].Function.Arguments) != `{"sql": "SELECT 1"}` {
		t.Errorf("arguments not preserved: %s", resp.Message.ToolCalls[0].Function.Arguments)
	}
	if resp.Thinking == "" || resp.PromptTokens != 120 || resp.CompletionTokens != 15 {
		t.Errorf("metadata not parsed: %+v", resp)
	}
}

func TestEmbedReturnsErrorOnStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "c", "e", false, 0).Embed(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error")
	}
}
