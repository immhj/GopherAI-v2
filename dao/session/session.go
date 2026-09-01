package session

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
)

// GetSessionsByUserName 按用户取会话，最新的在前。
// 注意：UserName 是字符串（系统生成的内部账号），此前这里误写成 int64，
// 且函数从未被调用过，所以类型错误一直没暴露。
func GetSessionsByUserName(userName string) ([]model.Session, error) {
	var sessions []model.Session
	err := mysql.DB.Where("user_name = ?", userName).
		Order("created_at DESC").
		Find(&sessions).Error
	return sessions, err
}

func CreateSession(session *model.Session) (*model.Session, error) {
	err := mysql.DB.Create(session).Error
	return session, err
}

func GetSessionByID(sessionID string) (*model.Session, error) {
	var session model.Session
	err := mysql.DB.Where("id = ?", sessionID).First(&session).Error
	return &session, err
}

// UpdateTitle 回填会话标题（标题是异步生成的，生成好后再写回）
func UpdateTitle(sessionID, title string) error {
	return mysql.DB.Model(&model.Session{}).
		Where("id = ?", sessionID).
		Update("title", title).Error
}
