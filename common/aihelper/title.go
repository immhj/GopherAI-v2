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

	// 把待概括的内容和指令放在同一条 user 消息里，并用分隔符界定。
	// 早先把指令写在 system 里时，模型有时会去概括"指令本身"，
	// 生成出「会话命名助手的任务」这类标题。
	prompt := "下面三重引号中是一段用户提问：\n\"\"\"\n" +
		truncateRunes(question, 200) +
		"\n\"\"\"\n\n请为这段提问起一个不超过5个字的中文标题，用于聊天记录列表。" +
		"直接输出标题文字本身，不要加引号、标点、前缀或任何说明。"

	raw, err := ChatCompletion(callCtx, resolveModel(model), []ChatMessage{
		TextMessage("user", prompt),
	})
	if err != nil {
		return fallback
	}

	title := cleanTitle(raw)
	if title == "" || looksLikeInstruction(title) {
		return fallback
	}
	return truncateRunes(title, titleMaxRunes)
}

// looksLikeInstruction 拦掉模型"复述任务"而非给出标题的情况
func looksLikeInstruction(s string) bool {
	for _, bad := range []string{"标题", "概括", "助手", "提问", "任务", "引号", "输出"} {
		if strings.Contains(s, bad) {
			return true
		}
	}
	return false
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
