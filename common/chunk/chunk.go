package chunk

import (
	"strings"
	"unicode/utf8"
)

// 文本切块。切块是纯文本处理，和向量模型无关：模型只负责把每一块转成向量。
//
// 策略：优先在语义边界断开（markdown 标题 > 空行段落 > 换行 > 句末标点），
// 尽量凑到目标大小；相邻块之间保留一段重叠，避免答案正好被切在边界上而检索不到。
//
// 长度按「字符数（rune）」计算，中文语料下比字节数直观。

// Split 把文本切成若干块。size 为目标块大小，overlap 为相邻块的重叠长度。
func Split(text string, size, overlap int) []string {
	if size <= 0 {
		size = 700
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 7
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}

	// 先按语义边界拆成小单元，再贪心地合并到接近目标大小
	units := splitIntoUnits(text)

	var (
		chunks  []string
		current strings.Builder
	)

	flush := func() {
		s := strings.TrimSpace(current.String())
		if s != "" {
			chunks = append(chunks, s)
		}
		current.Reset()
	}

	for _, unit := range units {
		unitLen := utf8.RuneCountInString(unit)

		// 单个单元就超过目标大小：先收尾，再把它硬切成定长片段
		if unitLen > size {
			flush()
			for _, piece := range hardSplit(unit, size, overlap) {
				chunks = append(chunks, piece)
			}
			continue
		}

		if utf8.RuneCountInString(current.String())+unitLen > size && current.Len() > 0 {
			prev := current.String()
			flush()
			// 用上一块的尾部作为下一块的开头，形成重叠
			if tail := lastRunes(strings.TrimSpace(prev), overlap); tail != "" {
				current.WriteString(tail)
				current.WriteString("\n")
			}
		}

		current.WriteString(unit)
		if !strings.HasSuffix(unit, "\n") {
			current.WriteString("\n")
		}
	}
	flush()

	return chunks
}

// splitIntoUnits 按语义边界把文本拆成不可再分的小单元。
// markdown 标题单独成行成为一个单元，保证标题不会和上一节的内容黏在一起。
func splitIntoUnits(text string) []string {
	var units []string

	for _, block := range strings.Split(text, "\n\n") {
		block = strings.Trim(block, "\n")
		if strings.TrimSpace(block) == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		var buf []string

		for _, line := range lines {
			if isHeading(line) {
				if len(buf) > 0 {
					units = append(units, strings.Join(buf, "\n"))
					buf = nil
				}
				units = append(units, line)
				continue
			}
			buf = append(buf, line)
		}
		if len(buf) > 0 {
			units = append(units, strings.Join(buf, "\n"))
		}
	}

	return units
}

func isHeading(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#")
}

// hardSplit 处理没有可用边界的超长文本：按长度切，并尽量退到最近的句末标点。
func hardSplit(s string, size, overlap int) []string {
	runes := []rune(s)
	var out []string

	step := size - overlap
	if step <= 0 {
		step = size
	}

	for start := 0; start < len(runes); start += step {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}

		piece := runes[start:end]

		// 未到结尾时，尝试在句末标点处收束，读起来更完整
		if end < len(runes) {
			if cut := lastSentenceEnd(piece); cut > size/2 {
				piece = piece[:cut]
				// 下一块的起点跟着调整
				step = cut - overlap
				if step <= 0 {
					step = cut
				}
			}
		}

		if t := strings.TrimSpace(string(piece)); t != "" {
			out = append(out, t)
		}

		if end == len(runes) {
			break
		}
	}

	return out
}

// lastSentenceEnd 返回最后一个句末标点之后的位置（找不到返回 -1）
func lastSentenceEnd(runes []rune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		switch runes[i] {
		case '。', '！', '？', '；', '\n', '.', '!', '?', ';':
			return i + 1
		}
	}
	return -1
}

// lastRunes 取字符串末尾 n 个字符
func lastRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}
