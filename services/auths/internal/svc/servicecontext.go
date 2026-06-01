package svc

import (
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/leventsg/e-commerce-AI-system/dal/model/user"
	"github.com/leventsg/e-commerce-AI-system/services/auths/internal/config"
)

type ServiceContext struct {
	Config    config.Config
	UserModel user.UsersModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	conn := sqlx.NewMysql(c.MysqlConfig.DataSource)
	return &ServiceContext{
		UserModel: user.NewUsersModel(conn),
		Config:    c,
	}
}
