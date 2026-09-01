package aihelper

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// 工具表（Tool Registry）是 agent 的核心接缝：
// 模型每次请求都会收到这张表的描述，由模型自己决定要不要调用、调哪个、参数填什么。
// 后续接入 MCP（把 MCP server 的 tools/list 翻译成 ToolDef 注册进来）或
// RAG（注册一个 search_documents 工具）都只需在这里 Register，agent 循环无需改动。

// ToolHandler 工具的实际执行逻辑。args 为模型给出的参数，返回值为回灌给模型的文本结果。
type ToolHandler func(ctx context.Context, args map[string]interface{}) (string, error)

// Tool 一个可被模型调用的工具：对外的声明 + 对内的实现
type Tool struct {
	Def     ToolDef
	Handler ToolHandler
}

var (
	toolRegistry = make(map[string]Tool)
	toolMu       sync.RWMutex
)

// RegisterTool 注册一个工具（重复名字会被覆盖）
func RegisterTool(name, description string, parameters map[string]interface{}, handler ToolHandler) {
	toolMu.Lock()
	defer toolMu.Unlock()
	toolRegistry[name] = Tool{
		Def: ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			},
		},
		Handler: handler,
	}
}

// AvailableTools 返回随请求发送给模型的工具声明（按名字排序，保证稳定）
func AvailableTools() []ToolDef {
	toolMu.RLock()
	defer toolMu.RUnlock()

	names := make([]string, 0, len(toolRegistry))
	for name := range toolRegistry {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]ToolDef, 0, len(names))
	for _, name := range names {
		defs = append(defs, toolRegistry[name].Def)
	}
	return defs
}

// ExecuteTool 按名字执行工具。rawArgs 是模型给出的 JSON 字符串参数。
func ExecuteTool(ctx context.Context, name string, rawArgs string) (string, error) {
	toolMu.RLock()
	tool, ok := toolRegistry[name]
	toolMu.RUnlock()

	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	args := map[string]interface{}{}
	if rawArgs != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", fmt.Errorf("invalid arguments for tool %s: %w", name, err)
		}
	}

	return tool.Handler(ctx, args)
}

func init() {
	registerBuiltinTools()
}

func registerBuiltinTools() {
	// get_current_time：模型本身不知道"现在"是什么时候，这个工具零依赖且真实有用，
	// 同时也用于验证整条 agent 工具调用链路是通的。
	RegisterTool(
		"get_current_time",
		"获取当前的日期和时间。当用户询问现在的时间、今天的日期，或需要基于当前时间进行计算时使用。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "IANA 时区名，例如 Asia/Shanghai、UTC。默认 Asia/Shanghai。",
				},
			},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			tz := "Asia/Shanghai"
			if v, ok := args["timezone"].(string); ok && v != "" {
				tz = v
			}
			loc, err := time.LoadLocation(tz)
			if err != nil {
				loc = time.UTC
				tz = "UTC"
			}
			now := time.Now().In(loc)
			return fmt.Sprintf("当前时间: %s (%s)", now.Format("2006-01-02 15:04:05 Monday"), tz), nil
		},
	)
}
