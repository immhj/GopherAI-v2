package aihelper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"GopherAI/config"
)

// 本文件实现一个精简的 OpenAI 兼容聊天客户端，直接对接 Aether 反代网关。
// 支持文本 / 多模态（图片）消息，以及流式 / 非流式两种模式。
// 通过切换 model 字段即可调用网关上的任意模型（claude-opus-5 / gpt-5.6-sol 等）。

// ChatMessage OpenAI 兼容消息。Content 可以是 string（纯文本）或 []ContentPart（多模态）
type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content,omitempty"`
	// 助手请求调用工具时携带
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// role=tool 的消息用它关联到对应的工具调用
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// ToolDef 发送给模型的工具声明（OpenAI function calling 格式）
type ToolDef struct {
	Type     string      `json:"type"` // 固定 "function"
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // JSON Schema
}

// ToolCall 模型请求的一次工具调用
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// AssistantTurn 模型一轮输出：可能是文本，也可能是要求调用工具
type AssistantTurn struct {
	Content   string
	ToolCalls []ToolCall
}

// ToolMessage 构造工具执行结果消息，回灌给模型
func ToolMessage(toolCallID, result string) ChatMessage {
	return ChatMessage{Role: "tool", ToolCallID: toolCallID, Content: result}
}

// ContentPart 多模态消息的一个片段（文本或图片）
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 图片内容，URL 支持 http(s) 链接或 data:image/...;base64,xxx
type ImageURL struct {
	URL string `json:"url"`
}

// TextMessage 构造纯文本消息
func TextMessage(role, text string) ChatMessage {
	return ChatMessage{Role: role, Content: text}
}

// MultimodalMessage 构造带图片的用户消息
func MultimodalMessage(text, imageURL string) ChatMessage {
	parts := []ContentPart{}
	if text != "" {
		parts = append(parts, ContentPart{Type: "text", Text: text})
	}
	parts = append(parts, ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: imageURL}})
	return ChatMessage{Role: "user", Content: parts}
}

func apiKey() string {
	return os.Getenv("ANTHROPIC_API_KEY")
}

func endpoint() string {
	base := strings.TrimRight(config.GetConfig().ModelServiceConfig.BaseUrl, "/")
	return base + "/chat/completions"
}

func newHTTPClient() *http.Client {
	// 流式响应可能很长，这里不设置整体超时，由 ctx 控制取消
	return &http.Client{}
}

func buildRequest(ctx context.Context, model string, messages []ChatMessage, stream bool, tools []ToolDef) (*http.Request, error) {
	body := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey())
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// streamChunk 流式增量。tool_calls 会分片到达，需要按 index 累积拼接。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// ChatCompletion 非流式请求，返回完整回复文本
func ChatCompletion(ctx context.Context, model string, messages []ChatMessage) (string, error) {
	req, err := buildRequest(ctx, model, messages, false, nil)
	if err != nil {
		return "", err
	}
	resp, err := newHTTPClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("model service request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model service returned %d: %s", resp.StatusCode, string(data))
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse model response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("model service error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("model service returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// ChatCompletionStream 流式请求（带工具声明）。
// 文本增量实时通过 cb 回调；若模型要求调用工具，则把累积好的 tool_calls 一并返回，
// 由调用方（agent 循环）执行工具并继续下一轮。
func ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage, tools []ToolDef, cb func(string)) (*AssistantTurn, error) {
	req, err := buildRequest(ctx, model, messages, true, tools)
	if err != nil {
		return nil, err
	}
	resp, err := newHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("model service request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("model service returned %d: %s", resp.StatusCode, string(data))
	}

	var full strings.Builder
	// 按 index 累积工具调用（id / name 只在首个分片出现，arguments 逐片拼接）
	acc := map[int]*ToolCall{}
	var order []int

	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if payload == "[DONE]" {
					break
				}
				var chunk streamChunk
				if jsonErr := json.Unmarshal([]byte(payload), &chunk); jsonErr == nil && len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta

					if delta.Content != "" {
						full.WriteString(delta.Content)
						cb(delta.Content)
					}

					for _, tc := range delta.ToolCalls {
						existing, ok := acc[tc.Index]
						if !ok {
							existing = &ToolCall{Type: "function"}
							acc[tc.Index] = existing
							order = append(order, tc.Index)
						}
						if tc.ID != "" {
							existing.ID = tc.ID
						}
						if tc.Type != "" {
							existing.Type = tc.Type
						}
						if tc.Function.Name != "" {
							existing.Function.Name = tc.Function.Name
						}
						if tc.Function.Arguments != "" {
							existing.Function.Arguments += tc.Function.Arguments
						}
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, fmt.Errorf("stream read error: %w", readErr)
		}
	}

	turn := &AssistantTurn{Content: full.String()}
	for _, idx := range order {
		turn.ToolCalls = append(turn.ToolCalls, *acc[idx])
	}
	return turn, nil
}
