package model

import (
	"time"

	"gorm.io/gorm"
)

// Document 一份用户上传的知识库文档。
// 文档内容被切块并向量化后存进向量库，这张表只保存归属关系和元信息。
type Document struct {
	ID         string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserName   string         `gorm:"index;not null;type:varchar(50)" json:"-"` // 归属用户（内部标识）
	Filename   string         `gorm:"type:varchar(255)" json:"filename"`        // 用户上传时的原始文件名
	StoredPath string         `gorm:"type:varchar(512)" json:"-"`               // 服务器上的存放路径
	SizeBytes  int64          `json:"size_bytes"`
	ChunkCount int            `json:"chunk_count"` // 切出来的块数
	CreatedAt  time.Time      `json:"created_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}
