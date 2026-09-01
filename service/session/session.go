package session

import (
	"GopherAI/common/aihelper"
	"GopherAI/common/code"
	"GopherAI/dao/session"
	"GopherAI/model"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

var ctx = context.Background()

// GetUserSessionsByUserName 取用户的会话列表。
// 数据源是数据库而不是内存中的 AIHelperManager：内存 map 的遍历顺序不固定，
// 会导致列表每次刷新都在跳动，而且标题本来就存在库里。
func GetUserSessionsByUserName(userName string) ([]model.SessionInfo, error) {
	sessions, err := session.GetSessionsByUserName(userName)
	if err != nil {
		return nil, err
	}

	infos := make([]model.SessionInfo, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		title := s.Title
		if title == "" {
			title = "新会话"
		}
		infos = append(infos, model.SessionInfo{
			SessionID: s.ID,
			Title:     title,
		})
	}

	return infos, nil
}

func CreateSessionAndSendMessage(userName string, userQuestion string, modelName string, imageURL string) (string, string, code.Code) {
	//1：创建一个新的会话
	newSession := &model.Session{
		ID:       uuid.New().String(),
		UserName: userName,
		Title:    fallbackTitle(userQuestion),
	}
	createdSession, err := session.CreateSession(newSession)
	if err != nil {
		log.Println("CreateSessionAndSendMessage CreateSession error:", err)
		return "", "", code.CodeServerBusy
	}

	//2：获取AIHelper
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userName, createdSession.ID)
	if err != nil {
		log.Println("CreateSessionAndSendMessage GetOrCreateAIHelper error:", err)
		return "", "", code.AIModelFail
	}

	//3：与回答并发生成短标题
	go GenerateAndStoreTitle(userName, createdSession.ID, modelName, userQuestion)

	//4：生成AI回复
	aiResponse, err_ := helper.GenerateResponse(userName, ctx, aihelper.ChatRequest{
		Model:    modelName,
		Question: userQuestion,
		ImageURL: imageURL,
	})
	if err_ != nil {
		log.Println("CreateSessionAndSendMessage GenerateResponse error:", err_)
		return "", "", code.AIModelFail
	}

	return createdSession.ID, aiResponse.Content, code.CodeSuccess
}

func CreateStreamSessionOnly(userName string, userQuestion string) (string, code.Code) {
	newSession := &model.Session{
		ID:       uuid.New().String(),
		UserName: userName,
		// 先存一个截取的兜底标题，短标题生成好之后会覆盖它。
		// 这样即使生成失败，列表里也有可读内容。
		Title: fallbackTitle(userQuestion),
	}
	createdSession, err := session.CreateSession(newSession)
	if err != nil {
		log.Println("CreateStreamSessionOnly CreateSession error:", err)
		return "", code.CodeServerBusy
	}
	return createdSession.ID, code.CodeSuccess
}

// fallbackTitle 截取提问作为兜底标题
func fallbackTitle(question string) string {
	q := strings.TrimSpace(question)
	if q == "" {
		return "新会话"
	}
	runes := []rune(q)
	if len(runes) > 10 {
		return string(runes[:10])
	}
	return q
}

// GenerateAndStoreTitle 生成短标题并写回数据库，返回最终标题。
// 由调用方与回答并发执行，避免给首个字增加延迟。
func GenerateAndStoreTitle(userName, sessionID, modelName, question string) string {
	title := aihelper.GenerateTitle(ctx, modelName, question)
	if title == "" {
		title = fallbackTitle(question)
	}
	if err := session.UpdateTitle(sessionID, title); err != nil {
		log.Println("GenerateAndStoreTitle UpdateTitle error:", err)
	}
	return title
}

func StreamMessageToExistingSession(userName string, sessionID string, userQuestion string, modelName string, imageURL string, writer http.ResponseWriter) code.Code {
	// 确保 writer 支持 Flush
	flusher, ok := writer.(http.Flusher)
	if !ok {
		log.Println("StreamMessageToExistingSession: streaming unsupported")
		return code.CodeServerBusy
	}

	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userName, sessionID)
	if err != nil {
		log.Println("StreamMessageToExistingSession GetOrCreateAIHelper error:", err)
		return code.AIModelFail
	}

	// 文本增量一律以 JSON 事件下发：data: {"content":"..."}
	// 不能直接把原文塞进 data:，否则
	//   1) 前后空格会在解析时丢失（英文单词会粘在一起）
	//   2) 文本里的换行会破坏 SSE 分帧，导致内容被静默丢弃（markdown 列表/代码块）
	// JSON 编码会把换行转义成 \n，空格原样保留。
	cb := func(msg string) {
		payload, err := json.Marshal(map[string]string{"content": msg})
		if err != nil {
			log.Println("[SSE] marshal content error:", err)
			return
		}
		if _, err := writer.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
			log.Println("[SSE] Write error:", err)
			return
		}
		flusher.Flush() //  每次必须 flush
	}

	// 工具调用通知：以 JSON 事件下发，前端据此展示"正在调用工具…"
	onTool := func(toolName string) {
		payload, err := json.Marshal(map[string]string{"tool": toolName})
		if err != nil {
			return
		}
		if _, err := writer.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
			log.Println("[SSE] tool notify write error:", err)
			return
		}
		flusher.Flush()
	}

	_, err_ := helper.StreamResponse(userName, ctx, cb, onTool, aihelper.ChatRequest{
		Model:    modelName,
		Question: userQuestion,
		ImageURL: imageURL,
	})
	if err_ != nil {
		log.Println("StreamMessageToExistingSession StreamResponse error:", err_)
		return code.AIModelFail
	}

	_, err = writer.Write([]byte("data: [DONE]\n\n"))
	if err != nil {
		log.Println("StreamMessageToExistingSession write DONE error:", err)
		return code.AIModelFail
	}
	flusher.Flush()

	return code.CodeSuccess
}

func CreateStreamSessionAndSendMessage(userName string, userQuestion string, modelName string, imageURL string, writer http.ResponseWriter) (string, code.Code) {

	sessionID, code_ := CreateStreamSessionOnly(userName, userQuestion)
	if code_ != code.CodeSuccess {
		return "", code_
	}

	code_ = StreamMessageToExistingSession(userName, sessionID, userQuestion, modelName, imageURL, writer)
	if code_ != code.CodeSuccess {

		return sessionID, code_
	}

	return sessionID, code.CodeSuccess
}

func ChatSend(userName string, sessionID string, userQuestion string, modelName string, imageURL string) (string, code.Code) {
	//1：获取AIHelper
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper(userName, sessionID)
	if err != nil {
		log.Println("ChatSend GetOrCreateAIHelper error:", err)
		return "", code.AIModelFail
	}

	//2：生成AI回复
	aiResponse, err_ := helper.GenerateResponse(userName, ctx, aihelper.ChatRequest{
		Model:    modelName,
		Question: userQuestion,
		ImageURL: imageURL,
	})
	if err_ != nil {
		log.Println("ChatSend GenerateResponse error:", err_)
		return "", code.AIModelFail
	}

	return aiResponse.Content, code.CodeSuccess
}

func GetChatHistory(userName string, sessionID string) ([]model.History, code.Code) {
	// 获取AIHelper中的消息历史
	manager := aihelper.GetGlobalManager()
	helper, exists := manager.GetAIHelper(userName, sessionID)
	if !exists {
		return nil, code.CodeServerBusy
	}

	messages := helper.GetMessages()
	history := make([]model.History, 0, len(messages))

	// 直接使用消息自身记录的角色，避免用奇偶位置去猜（一问一答被打破时会错乱）
	for _, msg := range messages {
		history = append(history, model.History{
			IsUser:  msg.IsUser,
			Content: msg.Content,
		})
	}

	return history, code.CodeSuccess
}

func ChatStreamSend(userName string, sessionID string, userQuestion string, modelName string, imageURL string, writer http.ResponseWriter) code.Code {

	return StreamMessageToExistingSession(userName, sessionID, userQuestion, modelName, imageURL, writer)
}
