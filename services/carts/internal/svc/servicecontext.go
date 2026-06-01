package svc

import (
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/leventsg/e-commerce-AI-system/dal/model/cart"
	"github.com/leventsg/e-commerce-AI-system/services/carts/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/carts/internal/db"
)

type ServiceContext struct {
	Config     config.Config
	Mysql      sqlx.SqlConn
	CartsModel cart.CartsModel
}

func NewServiceContext(c config.Config) (*ServiceContext, error) {
	mysql := db.NewMysql(c.MysqlConfig)
	return &ServiceContext{
		Config:     c,
		Mysql:      mysql,
		CartsModel: cart.NewCartsModel(mysql),
	}, nil
}
