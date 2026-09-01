package rag

import (
	"context"
	"fmt"
	"log"
	"strings"

	"GopherAI/common/chunk"
	"GopherAI/common/embedding"
	"GopherAI/common/qdrant"
	"GopherAI/config"

	"github.com/google/uuid"
)

// RAG 的两个动作：
//   Index    —— 把文档切块、向量化、写入向量库（上传时做一次）
//   Retrieve —— 把问题向量化，在该用户的文档范围内检索相关片段（提问时做）
//
// 检索结果不再无条件塞进提示词。它通过 search_documents 工具暴露给模型，
// 由模型自己判断某个问题是否需要查文档。

// IndexDocument 切块 + 向量化 + 入库，返回切出的块数
func IndexDocument(ctx context.Context, username, docID, filename, content string) (int, error) {
	cfg := config.GetConfig().RagConfig

	chunks := chunk.Split(content, cfg.RagChunkSize, cfg.RagChunkOverlap)
	if len(chunks) == 0 {
		return 0, fmt.Errorf("document has no indexable content")
	}
	log.Printf("[rag] indexing doc=%s file=%s chunks=%d", docID, filename, len(chunks))

	// 先确保集合存在（内部会探测向量维度）
	if err := qdrant.EnsureCollection(ctx); err != nil {
		return 0, err
	}

	vectors, err := embedding.EmbedTexts(ctx, chunks)
	if err != nil {
		return 0, fmt.Errorf("failed to embed document: %w", err)
	}

	points := make([]qdrant.Point, 0, len(chunks))
	for i, text := range chunks {
		points = append(points, qdrant.Point{
			ID:         uuid.New().String(),
			Vector:     vectors[i],
			Username:   username,
			DocID:      docID,
			Filename:   filename,
			ChunkIndex: i,
			Text:       text,
		})
	}

	if err := qdrant.Upsert(ctx, points); err != nil {
		return 0, fmt.Errorf("failed to store vectors: %w", err)
	}

	return len(chunks), nil
}

// DeleteDocument 删除某文档的全部向量
func DeleteDocument(ctx context.Context, username, docID string) error {
	return qdrant.DeleteByDoc(ctx, username, docID)
}

// Retrieve 在指定用户的文档里检索与 query 相关的片段
func Retrieve(ctx context.Context, username, query string, topK int) ([]qdrant.SearchHit, error) {
	cfg := config.GetConfig().RagConfig
	if topK <= 0 {
		topK = cfg.RagTopK
	}

	vec, err := embedding.EmbedText(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	return qdrant.Search(ctx, username, vec, topK, cfg.RagScoreThreshold)
}

// FormatHits 把检索结果整理成回灌给模型的文本。
// 没有命中时明确说明，让模型据此如实回答"资料里没有"，而不是硬编。
func FormatHits(hits []qdrant.SearchHit) string {
	if len(hits) == 0 {
		return "没有在你上传的文档中找到与该问题相关的内容。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("在你的文档中找到 %d 段相关内容：\n\n", len(hits)))
	for i, h := range hits {
		b.WriteString(fmt.Sprintf("[片段 %d | 来源: %s | 相似度: %.3f]\n%s\n\n", i+1, h.Filename, h.Score, h.Text))
	}
	return b.String()
}
