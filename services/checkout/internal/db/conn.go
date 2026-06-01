package db

import (
	"context"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/config"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func NewMysql(mysqlConf config.MysqlConfig) sqlx.SqlConn {
	mysql := sqlx.NewMysql(mysqlConf.DataSource)
	db, err := mysql.RawDB()
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(mysqlConf.Conntimeout))
	defer cancel()
	err = db.PingContext(ctx)
	if err != nil {
		panic(err)
	}

	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(10)
	return mysql

}
