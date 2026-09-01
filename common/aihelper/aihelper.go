package aihelper

import (
	"GopherAI/common/rabbitmq"
	"GopherAI/config"
	"GopherAI/model"
	"context"
	"log"
	"strings"
	"sync"
)

// AIHelper 一个会话的对话记忆。不再绑定固定模型：
// 具体使用哪个模型由每次请求携带（ChatRequest.Model），统一经 OpenAI 兼容网关调用。
type AIHelper struct {
	messages []*model.Message
	mu       sync.RWMutex
	//一个会话绑定一个AIHelper
	SessionID string
	saveFunc  func(*model.Message) (*model.Message, error)
}

// ChatRequest 一次对话请求
type ChatRequest struct {
	Model    string // 真实模型名，如 claude-opus-5；为空则用配置默认模型
	Question string // 用户问题
	ImageURL string // 可选：图片（data:image/...;base64 或 http 链接），存在则走多模态
}

// NewAIHelper 创建新的AIHelper实例
func NewAIHelper(SessionID string) *AIHelper {
	return &AIHelper{
		messages: make([]*model.Message, 0),
		//异步推送到消息队列中
		saveFunc: func(msg *model.Message) (*model.Message, error) {
			data := rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.UserName, msg.IsUser)
			err := rabbitmq.RMQMessage.Publish(data)
			return msg, err
		},
		SessionID: SessionID,
	}
}

// AddMessage 添加消息到内存中并调用自定义存储函数
func (a *AIHelper) AddMessage(Content string, UserName string, IsUser bool, Save bool) {
	userMsg := model.Message{
		SessionID: a.SessionID,
		Content:   Content,
		UserName:  UserName,
		IsUser:    IsUser,
	}
	a.messages = append(a.messages, &userMsg)
	if Save {
		a.saveFunc(&userMsg)
	}
}

// SetSaveFunc 通过传入func，自己调用外部的保存函数，即可支持同步异步等多种策略
func (a *AIHelper) SetSaveFunc(saveFunc func(*model.Message) (*model.Message, error)) {
	a.saveFunc = saveFunc
}

// GetMessages 获取所有消息历史
func (a *AIHelper) GetMessages() []*model.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

func resolveModel(m string) string {
	if m != "" {
		return m
	}
	return config.GetConfig().ModelServiceConfig.DefaultModel
}

// buildRequestMessages 把历史记录转成网关消息格式，并对最后一条用户消息按需做 RAG 增强 / 附加图片。
// 约定：调用前用户问题已通过 AddMessage 追加到 a.messages 末尾。
func (a *AIHelper) buildRequestMessages(ctx context.Context, userName string, req ChatRequest) []ChatMessage {
	a.mu.RLock()
	history := make([]*model.Message, len(a.messages))
	copy(history, a.messages)
	a.mu.RUnlock()

	out := make([]ChatMessage, 0, len(history))
	// 历史消息（不含最后一条刚加入的用户问题）原样转换为文本消息
	for i := 0; i < len(history)-1; i++ {
		role := "assistant"
		if history[i].IsUser {
			role = "user"
		}
		out = append(out, TextMessage(role, history[i].Content))
	}

	// 组装最后一条用户消息。
	// 这里不再主动把文档内容塞进提示词：检索已经作为 search_documents 工具暴露，
	// 由模型自己判断该不该查。
	if req.ImageURL != "" {
		out = append(out, MultimodalMessage(req.Question, req.ImageURL))
	} else {
		out = append(out, TextMessage("user", req.Question))
	}
	return out
}

// maxAgentSteps 限制工具调用的迭代轮数，防止模型陷入死循环
const maxAgentSteps = 5

// StreamCallback 文本增量回调
type StreamCallback func(msg string)

// ToolNotifyCallback 工具被调用时的通知回调（用于前端展示"正在调用工具…"）
type ToolNotifyCallback func(toolName string)

// runAgent 执行 agent 工具调用循环：
//  1. 把工具表随消息发给模型，模型自行决定是否调用工具
//  2. 若模型要求调用，则本地执行工具、把结果作为 role=tool 消息回灌
//  3. 重复直到模型不再请求工具（或达到 maxAgentSteps）
//
// 文本增量始终通过 cb 实时输出，因此流式与非流式共用这一套逻辑
// （非流式只需传入一个空的 cb）。
func (a *AIHelper) runAgent(ctx context.Context, userName string, req ChatRequest, cb StreamCallback, onTool ToolNotifyCallback) (string, error) {
	// 把用户标识注入 context，供需要归属隔离的工具（如 search_documents）使用。
	// 工具参数里不暴露用户标识，模型无法指定查谁的文档。
	ctx = WithUserName(ctx, userName)

	messages := a.buildRequestMessages(ctx, userName, req)
	tools := AvailableTools()
	modelName := resolveModel(req.Model)

	var answer strings.Builder

	for step := 0; step < maxAgentSteps; step++ {
		turn, err := ChatCompletionStream(ctx, modelName, messages, tools, cb)
		if err != nil {
			return answer.String(), err
		}

		answer.WriteString(turn.Content)

		// 模型没有请求工具 -> 本轮内容即最终答案
		if len(turn.ToolCalls) == 0 {
			return answer.String(), nil
		}

		// 记录助手这一轮的工具调用请求
		messages = append(messages, ChatMessage{
			Role:      "assistant",
			Content:   turn.Content,
			ToolCalls: turn.ToolCalls,
		})

		// 逐个执行工具，把结果回灌给模型
		for _, tc := range turn.ToolCalls {
			if onTool != nil {
				onTool(tc.Function.Name)
			}
			log.Printf("[agent] step=%d calling tool=%s args=%s", step, tc.Function.Name, tc.Function.Arguments)

			result, execErr := ExecuteTool(ctx, tc.Function.Name, tc.Function.Arguments)
			if execErr != nil {
				// 工具失败不中断对话，把错误告诉模型让它自己决定怎么办
				log.Printf("[agent] tool %s failed: %v", tc.Function.Name, execErr)
				result = "工具执行失败: " + execErr.Error()
			}
			messages = append(messages, ToolMessage(tc.ID, result))
		}
	}

	log.Printf("[agent] reached max steps (%d) for session=%s", maxAgentSteps, a.SessionID)
	return answer.String(), nil
}

// GenerateResponse 同步生成（内部同样走 agent 循环，只是不对外输出增量）
func (a *AIHelper) GenerateResponse(userName string, ctx context.Context, req ChatRequest) (*model.Message, error) {
	a.AddMessage(req.Question, userName, true, true)

	content, err := a.runAgent(ctx, userName, req, func(string) {}, nil)
	if err != nil {
		return nil, err
	}

	modelMsg := &model.Message{
		SessionID: a.SessionID,
		UserName:  userName,
		Content:   content,
		IsUser:    false,
	}
	a.AddMessage(modelMsg.Content, userName, false, true)
	return modelMsg, nil
}

// StreamResponse 流式生成
func (a *AIHelper) StreamResponse(userName string, ctx context.Context, cb StreamCallback, onTool ToolNotifyCallback, req ChatRequest) (*model.Message, error) {
	a.AddMessage(req.Question, userName, true, true)

	content, err := a.runAgent(ctx, userName, req, cb, onTool)
	if err != nil {
		return nil, err
	}

	modelMsg := &model.Message{
		SessionID: a.SessionID,
		UserName:  userName,
		Content:   content,
		IsUser:    false,
	}
	a.AddMessage(modelMsg.Content, userName, false, true)
	return modelMsg, nil
}
