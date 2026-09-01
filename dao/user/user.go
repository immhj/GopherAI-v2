package user

import (
	"GopherAI/common/mysql"
	"GopherAI/model"
	"GopherAI/utils"
	"context"

	"gorm.io/gorm"
)

const (
	CodeMsg     = "GopherAI验证码如下(验证码仅限于2分钟有效): "
	UserNameMsg = "GopherAI的账号如下，请保留好，后续可以用账号进行登录 "
)

var ctx = context.Background()

// IsExistUserByEmail 按邮箱判断用户是否存在（登录、注册查重均使用邮箱）
func IsExistUserByEmail(email string) (bool, *model.User) {

	user, err := mysql.GetUserByEmail(email)

	if err == gorm.ErrRecordNotFound || user == nil {
		return false, nil
	}

	return true, user
}

// Register 注册新用户：nickname 为用户填写的昵称，username 为系统生成的内部账号
func Register(nickname, username, email, password string) (*model.User, bool) {
	if user, err := mysql.InsertUser(&model.User{
		Email:    email,
		Nickname: nickname,
		Username: username,
		Password: utils.MD5(password),
	}); err != nil {
		return nil, false
	} else {
		return user, true
	}
}
