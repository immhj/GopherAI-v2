package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"GopherAI/common/embedding"
	"GopherAI/config"
)

// Qdrant 向量库客户端（走 REST API，不引入额外 SDK）。
//
// 集合布局：所有用户的所有文档共用一个 collection，每个向量的 payload 里带
// username / doc_id / filename / chunk_index / text。检索时由服务端强制按
// username 过滤 —— 归属是显式的，隔离由服务端保证，调用方无法越权。

const requestTimeout = 30 * time.Second

var (
	ensureOnce sync.Once
	ensureErr  error
)

func baseURL() string {
	return strings.TrimRight(config.GetConfig().QdrantConfig.QdrantUrl, "/")
}

func collection() string {
	c := config.GetConfig().QdrantConfig.QdrantCollection
	if c == "" {
		c = "documents"
	}
	return c
}

func doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(raw)
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, baseURL()+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("qdrant request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// EnsureCollection 确保集合存在。维度不写死，而是先探测 embedding 模型的实际输出长度，
// 避免配置与模型不一致导致后续写入全部失败。只会执行一次。
func EnsureCollection(ctx context.Context) error {
	ensureOnce.Do(func() {
		ensureErr = ensureCollection(ctx)
	})
	return ensureErr
}

func ensureCollection(ctx context.Context) error {
	name := collection()

	// 已存在则直接复用
	_, status, err := doRequest(ctx, http.MethodGet, "/collections/"+name, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}

	dim, err := embedding.Dimension(ctx)
	if err != nil {
		return fmt.Errorf("failed to probe embedding dimension: %w", err)
	}
	log.Printf("[qdrant] creating collection %s with dimension %d", name, dim)

	body := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     dim,
			"distance": "Cosine",
		},
	}
	data, status, err := doRequest(ctx, http.MethodPut, "/collections/"+name, body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("failed to create collection: %s", string(data))
	}

	// 给过滤字段建索引，让按 username / doc_id 的过滤走索引而不是全量扫描
	for _, field := range []string{"username", "doc_id"} {
		idxBody := map[string]interface{}{
			"field_name":   field,
			"field_schema": "keyword",
		}
		if data, status, err := doRequest(ctx, http.MethodPut, "/collections/"+name+"/index?wait=true", idxBody); err != nil {
			return err
		} else if status != http.StatusOK {
			log.Printf("[qdrant] warning: failed to index field %s: %s", field, string(data))
		}
	}

	return nil
}

// Point 一个待写入的向量点
type Point struct {
	ID         string
	Vector     []float32
	Username   string
	DocID      string
	Filename   string
	ChunkIndex int
	Text       string
}

// Upsert 批量写入向量
func Upsert(ctx context.Context, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	if err := EnsureCollection(ctx); err != nil {
		return err
	}

	items := make([]map[string]interface{}, 0, len(points))
	for _, p := range points {
		items = append(items, map[string]interface{}{
			"id":     p.ID,
			"vector": p.Vector,
			"payload": map[string]interface{}{
				"username":    p.Username,
				"doc_id":      p.DocID,
				"filename":    p.Filename,
				"chunk_index": p.ChunkIndex,
				"text":        p.Text,
			},
		})
	}

	body := map[string]interface{}{"points": items}
	data, status, err := doRequest(ctx, http.MethodPut, "/collections/"+collection()+"/points?wait=true", body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("qdrant upsert failed: %s", string(data))
	}
	return nil
}

// SearchHit 一条检索结果
type SearchHit struct {
	Score    float32
	Text     string
	Filename string
	DocID    string
}

type searchResponse struct {
	Result []struct {
		Score   float32 `json:"score"`
		Payload struct {
			Text     string `json:"text"`
			Filename string `json:"filename"`
			DocID    string `json:"doc_id"`
		} `json:"payload"`
	} `json:"result"`
	Status interface{} `json:"status"`
}

// Search 在指定用户的文档范围内做向量检索。
// username 由调用方（服务端）从登录态传入，绝不能来自用户输入或模型参数。
func Search(ctx context.Context, username string, vector []float32, topK int, scoreThreshold float32) ([]SearchHit, error) {
	if err := EnsureCollection(ctx); err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 5
	}

	body := map[string]interface{}{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
		"filter": map[string]interface{}{
			"must": []map[string]interface{}{
				{"key": "username", "match": map[string]interface{}{"value": username}},
			},
		},
	}
	if scoreThreshold > 0 {
		body["score_threshold"] = scoreThreshold
	}

	data, status, err := doRequest(ctx, http.MethodPost, "/collections/"+collection()+"/points/search", body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("qdrant search failed: %s", string(data))
	}

	var parsed searchResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse qdrant response: %w", err)
	}

	hits := make([]SearchHit, 0, len(parsed.Result))
	for _, r := range parsed.Result {
		hits = append(hits, SearchHit{
			Score:    r.Score,
			Text:     r.Payload.Text,
			Filename: r.Payload.Filename,
			DocID:    r.Payload.DocID,
		})
	}
	return hits, nil
}

// DeleteByDoc 删除某个文档的所有向量（按 username + doc_id 双重限定，防止越权删除）
func DeleteByDoc(ctx context.Context, username, docID string) error {
	if err := EnsureCollection(ctx); err != nil {
		return err
	}

	body := map[string]interface{}{
		"filter": map[string]interface{}{
			"must": []map[string]interface{}{
				{"key": "username", "match": map[string]interface{}{"value": username}},
				{"key": "doc_id", "match": map[string]interface{}{"value": docID}},
			},
		},
	}
	data, status, err := doRequest(ctx, http.MethodPost, "/collections/"+collection()+"/points/delete?wait=true", body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("qdrant delete failed: %s", string(data))
	}
	return nil
}
