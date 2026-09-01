package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"GopherAI/config"
)

// 网络搜索客户端（Tavily）。
// 选它的原因是返回值本来就是给模型消费的：除了结果条目，还带一段直接可用的
// answer 摘要，不需要在这边做 HTML 解析。

const searchTimeout = 25 * time.Second

type Result struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

type Response struct {
	Answer  string   `json:"answer"`
	Results []Result `json:"results"`
	Error   string   `json:"error"`
}

func apiKey() string {
	return os.Getenv("TAVILY_API_KEY")
}

// Search 执行一次网络搜索
func Search(ctx context.Context, query string, maxResults int) (*Response, error) {
	cfg := config.GetConfig().SearchConfig

	if apiKey() == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY is not set, web search is unavailable")
	}
	if maxResults <= 0 {
		maxResults = cfg.SearchMaxResults
	}
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 10 {
		maxResults = 10
	}

	payload := map[string]interface{}{
		"query":          query,
		"max_results":    maxResults,
		"include_answer": true,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(cfg.SearchBaseUrl, "/")
	if base == "" {
		base = "https://api.tavily.com"
	}

	reqCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/search", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search service returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed Response
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("search service error: %s", parsed.Error)
	}

	return &parsed, nil
}

// Format 把搜索结果整理成回灌给模型的文本。
// 带上 URL，方便模型在需要全文时接着用 fetch_url 抓取。
func Format(r *Response) string {
	if r == nil || (len(r.Results) == 0 && r.Answer == "") {
		return "没有搜索到相关结果。"
	}

	var b strings.Builder
	if r.Answer != "" {
		b.WriteString("摘要：")
		b.WriteString(r.Answer)
		b.WriteString("\n\n")
	}
	if len(r.Results) > 0 {
		b.WriteString(fmt.Sprintf("搜索结果（%d 条）：\n\n", len(r.Results)))
		for i, item := range r.Results {
			b.WriteString(fmt.Sprintf("[%d] %s\n链接: %s\n摘要: %s\n\n",
				i+1, item.Title, item.URL, item.Content))
		}
		b.WriteString("如需某条的完整内容，可以用 fetch_url 抓取对应链接。")
	}
	return b.String()
}
