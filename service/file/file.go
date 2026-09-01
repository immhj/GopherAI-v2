package file

import (
	"context"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"

	"GopherAI/common/rag"
	docDao "GopherAI/dao/document"
	"GopherAI/model"
	"GopherAI/utils"

	"github.com/google/uuid"
)

// UploadRagFile 上传一份知识库文档：存盘 -> 切块向量化 -> 记录元信息。
// 支持多文档：每次上传都是新增，不再覆盖旧文件。
func UploadRagFile(username string, file *multipart.FileHeader) (*model.Document, error) {
	if err := utils.ValidateFile(file); err != nil {
		log.Printf("File validation failed: %v", err)
		return nil, err
	}

	userDir := filepath.Join("uploads", username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		log.Printf("Failed to create user directory %s: %v", userDir, err)
		return nil, err
	}

	docID := uuid.New().String()
	ext := filepath.Ext(file.Filename)
	storedPath := filepath.Join(userDir, docID+ext)

	// 落盘
	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		return nil, err
	}
	defer src.Close()

	dst, err := os.Create(storedPath)
	if err != nil {
		log.Printf("Failed to create destination file %s: %v", storedPath, err)
		return nil, err
	}
	written, copyErr := io.Copy(dst, src)
	dst.Close()
	if copyErr != nil {
		log.Printf("Failed to copy file content: %v", copyErr)
		os.Remove(storedPath)
		return nil, copyErr
	}

	content, err := os.ReadFile(storedPath)
	if err != nil {
		os.Remove(storedPath)
		return nil, err
	}

	// 切块 + 向量化 + 入向量库
	ctx := context.Background()
	chunkCount, err := rag.IndexDocument(ctx, username, docID, file.Filename, string(content))
	if err != nil {
		log.Printf("Failed to index document: %v", err)
		// 索引失败就不要留下孤儿文件和半截向量
		os.Remove(storedPath)
		_ = rag.DeleteDocument(ctx, username, docID)
		return nil, err
	}

	doc := &model.Document{
		ID:         docID,
		UserName:   username,
		Filename:   file.Filename,
		StoredPath: storedPath,
		SizeBytes:  written,
		ChunkCount: chunkCount,
	}
	if err := docDao.Create(doc); err != nil {
		log.Printf("Failed to save document record: %v", err)
		os.Remove(storedPath)
		_ = rag.DeleteDocument(ctx, username, docID)
		return nil, err
	}

	log.Printf("Document indexed: user=%s file=%s chunks=%d", username, file.Filename, chunkCount)
	return doc, nil
}

// ListDocuments 列出用户的文档
func ListDocuments(username string) ([]model.Document, error) {
	return docDao.ListByUser(username)
}

// DeleteDocument 删除文档：向量、文件、记录一并清掉。
// 先按 username + id 取记录，取不到就说明不是这个用户的文档。
func DeleteDocument(username, docID string) error {
	doc, err := docDao.GetOwned(username, docID)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if err := rag.DeleteDocument(ctx, username, docID); err != nil {
		// 向量删除失败就中止，避免留下检索得到但已无文件的内容
		log.Printf("Failed to delete vectors for doc %s: %v", docID, err)
		return err
	}

	if doc.StoredPath != "" {
		if err := os.Remove(doc.StoredPath); err != nil && !os.IsNotExist(err) {
			log.Printf("Failed to remove file %s: %v", doc.StoredPath, err)
		}
	}

	return docDao.Delete(username, docID)
}
