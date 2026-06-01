package audit_log

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/dal/model/audit"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/event"
	"github.com/zeromicro/go-zero/core/logx"
)

type AuditLogConsumer struct {
	model    audit.AuditModel
	esClient *elasticsearch.Client
}

func NewAuditLogConsumer(model audit.AuditModel, esClient *elasticsearch.Client) *AuditLogConsumer {
	return &AuditLogConsumer{
		model:    model,
		esClient: esClient,
	}
}

func (c *AuditLogConsumer) Handle(ctx context.Context, msg []byte) error {
	var event event.AuditLog
	if err := json.Unmarshal(msg, &event); err != nil {
		return err
	}
	logx.Infow("audit log consumer start", logx.Field("event", event))
	// 入mysql和es，保证数据一致性
	if err := c.persistData(ctx, event); err != nil {
		logx.Errorw("insert failed, rejecting message", logx.Field("err", err), logx.Field("event", event))
		return err
	}
	return nil
}

func (c *AuditLogConsumer) persistData(ctx context.Context, data event.AuditLog) error {
	exist, err := c.model.CheckExistByTraceID(ctx, data.TraceID)
	if err != nil {
		return err
	}
	if !exist {
		if _, err := c.ToMysql(ctx, data); err != nil {
			return err
		}
	}
	if err := c.ToEs(ctx, data); err != nil {
		return err
	}
	return nil
}

func (c *AuditLogConsumer) ToMysql(ctx context.Context, data event.AuditLog) (int64, error) {
	res, err := c.model.Insert(ctx, &audit.Audit{
		UserId:      uint64(data.UserID),
		TargetId:    uint64(data.TargetID),
		TargetTable: data.TargetTable,
		ActionType:  data.ActionType,
		ClientIp:    data.ClientIP,
		ServiceName: data.ServiceName,
		ActionDesc:  sql.NullString{String: data.ActionDesc, Valid: data.ActionDesc != ""},
		OldData:     sql.NullString{String: data.OldData, Valid: data.OldData != ""},
		NewData:     sql.NullString{String: data.NewData, Valid: data.NewData != ""},

		SpanId:    data.SpanID,
		TraceId:   data.TraceID,
		CreatedAt: time.Unix(data.CreatedAt, 0),
	})
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (c *AuditLogConsumer) ToEs(ctx context.Context, data event.AuditLog) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}
	// 创建Index请求
	req := esapi.IndexRequest{
		Index:      biz.EsIndexName,
		DocumentID: data.TraceID,
		Body:       bytes.NewReader(jsonData), // 需要实现AuditReq的JSON序列化方法
	}

	// 执行请求
	res, err := req.Do(ctx, c.esClient)
	if err != nil {
		return fmt.Errorf("IndexRequest failed: %w", err)
	}
	defer res.Body.Close()

	// 检查响应状态
	if res.IsError() {
		return fmt.Errorf("elasticsearch error: %s", res.String())
	}
	return nil
}
