package svc

import (
	"github.com/leventsg/e-commerce-AI-system/dal/model/audit"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/mq"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config     config.Config
	AuditMQ    *mq.AuditMQ
	AuditModel audit.AuditModel
}

func NewServiceContext(c config.Config) *ServiceContext {
	auditMq, err := mq.Init(c)
	if err != nil {
		logx.Error(err)
		panic(err)
	}
	return &ServiceContext{
		Config:     c,
		AuditMQ:    auditMq,
		AuditModel: audit.NewAuditModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
	}
}
