package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"GopherAI/config"
)

// 火山方舟（Volcengine Ark）文档向量化客户端。
//
// 两个实测得出的约束决定了这里的实现方式：
//  1. 只能走 /embeddings/multimodal 端点。纯文本的 embedding 模型（doubao-embedding-text-*、
//     doubao-embedding-large-text-*）已经下线，用 /embeddings 调 vision 模型会被拒绝。
//     vision 模型接受纯文本输入，用于文本 RAG 没有问题。
//  2. 该接口一次只处理一条输入 —— 传多个片段会被合并成一个向量，而不是分别返回。
//     因此批量向量化只能靠并发发多次请求，不能靠批处理参数。
const embedTimeout = 60 * time.Second

type multimodalPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type embedRequest struct {
	Model      string           `json:"model"`
	Input      []multimodalPart `json:"input"`
	Dimensions int              `json:"dimensions,omitempty"`
}

type embedResponse struct {
	Data struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func apiKey() string {
	return os.Getenv("ARK_API_KEY")
}

func endpoint() string {
	base := strings.TrimRight(config.GetConfig().EmbeddingConfig.EmbeddingBaseUrl, "/")
	return base + "/embeddings/multimodal"
}

// EmbedText 把一段文本转成向量
func EmbedText(ctx context.Context, text string) ([]float32, error) {
	cfg := config.GetConfig().EmbeddingConfig

	if apiKey() == "" {
		return nil, fmt.Errorf("ARK_API_KEY is not set, cannot embed documents")
	}

	payload := embedRequest{
		Model:      cfg.EmbeddingModel,
		Input:      []multimodalPart{{Type: "text", Text: text}},
		Dimensions: cfg.EmbeddingDimensions,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding service returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed embedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse embedding response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embedding service error: %s", parsed.Error.Message)
	}
	if len(parsed.Data.Embedding) == 0 {
		return nil, fmt.Errorf("embedding service returned an empty vector")
	}

	return parsed.Data.Embedding, nil
}

// Dimension 探测当前模型实际输出的向量维度。
// 用探测值建向量库集合，避免把维度写死之后和模型不一致导致写入失败。
func Dimension(ctx context.Context) (int, error) {
	vec, err := EmbedText(ctx, "dimension probe")
	if err != nil {
		return 0, err
	}
	return len(vec), nil
}

// EmbedTexts 并发向量化多段文本，返回顺序与入参一致。
// 接口不支持批量，所以这里用有限并发替代批处理；任何一段失败则整体失败。
func EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	concurrency := config.GetConfig().EmbeddingConfig.EmbeddingConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	out := make([][]float32, len(texts))
	sem := make(chan struct{}, concurrency)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	// 出现错误后取消其余请求，避免白白消耗额度
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, text := range texts {
		wg.Add(1)
		go func(idx int, content string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if runCtx.Err() != nil {
				return
			}

			vec, err := embedWithRetry(runCtx, content)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("chunk %d: %w", idx, err)
					cancel()
				}
				return
			}
			out[idx] = vec
		}(i, text)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// embedWithRetry 失败后重试一次，抵御偶发的网络抖动或限流
func embedWithRetry(ctx context.Context, text string) ([]float32, error) {
	vec, err := EmbedText(ctx, text)
	if err == nil {
		return vec, nil
	}
	if ctx.Err() != nil {
		return nil, err
	}

	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return EmbedText(ctx, text)
}
