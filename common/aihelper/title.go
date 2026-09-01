package aihelper

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// 会话标题：用一次极短的模型调用把首个提问概括成几个字，用于侧边栏展示。
//
// 三重保险，保证列表永远有可读内容：
//  1. 提示词要求不超过 5 个字
//  2. 代码里硬截断到 8 个字（模型经常不听话，会写成一句话）
//  3. 调用失败或超时则退回截取提问本身
const (
	titleMaxRunes = 8
	titleFallback = 10
	titleTimeout  = 8 * time.Second
)

// GenerateTitle 为一段提问生成短标题。任何异常都会退回到截取提问，不会返回空串。
func GenerateTitle(ctx context.Context, model, question string) string {
	fallback := truncateRunes(strings.TrimSpace(question), titleFallback)

	if strings.TrimSpace(question) == "" {
		return "新会话"
	}

	callCtx, cancel := context.WithTimeout(ctx, titleTimeout)
	defer cancel()

	messages := []ChatMessage{
		TextMessage("system", "你是一个会话命名助手。请用不超过5个字概括用户这句话的主题，"+
			"作为聊天记录的标题。只输出标题本身，不要引号、标点、解释或任何多余内容。"),
		TextMessage("user", truncateRunes(question, 200)),
	}

	raw, err := ChatCompletion(callCtx, resolveModel(model), messages)
	if err != nil {
		return fallback
	}

	title := cleanTitle(raw)
	if title == "" {
		return fallback
	}
	return truncateRunes(title, titleMaxRunes)
}

// cleanTitle 去掉模型可能带上的引号、标点和换行
func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	// 只取第一行，模型有时会多输出解释
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.Trim(s, " \t\"'`“”‘’《》〈〉【】[]()（）:：.。,，!！?？-—_*#")
	return strings.TrimSpace(s)
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
