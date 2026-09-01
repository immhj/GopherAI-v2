package model

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	Email     string         `gorm:"type:varchar(100);uniqueIndex" json:"email"`   // 登录凭证，唯一
	Nickname  string         `gorm:"type:varchar(50)" json:"nickname"`             // 显示昵称，注册时填写、界面展示
	Username  string         `gorm:"type:varchar(50);uniqueIndex" json:"username"` // 系统生成的账号，内部唯一标识，用户不可见
	Password  string         `gorm:"type:varchar(255)" json:"-"`                   // 不返回给前端
	CreatedAt time.Time      `json:"created_at"`                                   // 自动时间戳
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"` // 支持软删除
}
