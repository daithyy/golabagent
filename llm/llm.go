// Package llm is a minimal client for Ollama REST API
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	http       *http.Client
	chatModel  string
	embedModel string
	think      bool
	numCtx     int
}

func New(baseURL, chatModel, embedModel string, think bool, numCtx int) *Client {
	return &Client{
		baseURL:    baseURL,
		http:       &http.Client{Timeout: 10 * time.Minute},
		chatModel:  chatModel,
		embedModel: embedModel,
		think:      think,
		numCtx:     numCtx,
	}
}

func (c *Client) EmbedModel() string {
	return c.embedModel
}

type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type ChatResponse struct {
	Message          Message `json:"message"`
	Thinking         string
	PromptTokens     int
	CompletionTokens int
	Duration         time.Duration
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Tools    []Tool         `json:"tools,omitempty"`
	Stream   bool           `json:"stream"`
	Think    bool           `json:"think"`
	Options  map[string]any `json:"options"`
}

type chatResponse struct {
	Message struct {
		Message
		Thinking string `json:"thinking"`
	} `json:"message"`
	PromptEvalCount int   `json:"prompt_eval_count"`
	EvalCount       int   `json:"eval_count"`
	TotalDuration   int64 `json:"total_duration"`
}

func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (ChatResponse, error) {
	req := chatRequest{
		Model:    c.chatModel,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Think:    c.think,
		Options: map[string]any{
			"temperature": 0,
			"num_ctx":     c.numCtx,
		},
	}

	var resp chatResponse
	if err := c.post(ctx, "/api/chat", req, &resp); err != nil {
		return ChatResponse{}, err
	}

	return ChatResponse{
		Message:          resp.Message.Message,
		Thinking:         resp.Message.Thinking,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
		Duration:         time.Duration(resp.TotalDuration),
	}, nil
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	var resp embedResponse
	if err := c.post(ctx, "/api/embed", embedRequest{Model: c.embedModel, Input: inputs}, &resp); err != nil {
		return nil, err
	}

	if len(resp.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(resp.Embeddings), len(inputs))
	}

	return resp.Embeddings, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	var err error

	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", path, err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+path,
		bytes.NewReader(b),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama %s: %w", path, err)
	}
	defer func() {
		if e := res.Body.Close(); err != nil {
			err = e
		}
	}()

	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("ollama %s: %s: %s", path, res.Status, bytes.TrimSpace(msg))
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}

	return err
}
