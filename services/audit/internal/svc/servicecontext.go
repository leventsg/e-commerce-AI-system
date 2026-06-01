package svc

import (
	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/dal/model/audit"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/config"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ServiceContext struct {
	Config     config.Config
	AuditModel audit.AuditModel
	Producer   mq.Producer
}

func NewServiceContext(c config.Config) *ServiceContext {
	producer, err := mq.NewKafkaProducer(c.KafkaMQ)
	if err != nil {
		logx.Error(err)
		panic(err)
	}
	return &ServiceContext{
		Config:     c,
		AuditModel: audit.NewAuditModel(sqlx.NewMysql(c.MysqlConfig.DataSource)),
		Producer:   producer,
	}
}
