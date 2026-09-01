package document

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
)

// Create 保存文档元信息
func Create(doc *model.Document) error {
	return mysql.DB.Create(doc).Error
}

// ListByUser 列出某个用户的所有文档，最新的在前
func ListByUser(userName string) ([]model.Document, error) {
	var docs []model.Document
	err := mysql.DB.Where("user_name = ?", userName).Order("created_at DESC").Find(&docs).Error
	return docs, err
}

// GetOwned 取某个用户名下的指定文档。
// 查询条件同时限定 id 和 user_name，避免拿到别人的文档。
func GetOwned(userName, id string) (*model.Document, error) {
	doc := new(model.Document)
	err := mysql.DB.Where("id = ? AND user_name = ?", id, userName).First(doc).Error
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// Delete 删除文档记录（软删除）
func Delete(userName, id string) error {
	return mysql.DB.Where("id = ? AND user_name = ?", id, userName).Delete(&model.Document{}).Error
}

// UpdateChunkCount 回填切块数量
func UpdateChunkCount(id string, count int) error {
	return mysql.DB.Model(&model.Document{}).Where("id = ?", id).Update("chunk_count", count).Error
}
