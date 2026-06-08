package audit_log

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/leventsg/e-commerce-AI-system/common/consts/biz"
	"github.com/leventsg/e-commerce-AI-system/common/mq"
	"github.com/leventsg/e-commerce-AI-system/dal/model/audit"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/consumer/registry"
	"github.com/leventsg/e-commerce-AI-system/services/audit/model/es"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func init() {
	registry.Register("audit_log", Init)
}

func Init(c config.Config) error {
	//mysql conn

	model := audit.NewAuditModel(sqlx.NewMysql(c.MysqlConfig.DataSource))
	// es client

	cfg := elasticsearch.Config{
		Addresses: []string{c.ElasticSearch.Addr},
	}
	esClient, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return err
	}
	if err := initEsIndex(context.Background(), esClient); err != nil {
		return err
	}

	kafkaConf, err := c.KafkaMQ.TopicConfig("AuditLog")
	if err != nil {
		return err
	}

	consumer, err := mq.NewKafkaConsumer(kafkaConf)
	if err != nil {
		return err
	}
	handler := NewAuditLogConsumer(model, esClient)

	go func() {
		if err := consumer.Consume(context.Background(), kafkaConf.Topic, kafkaConf.Group, handler); err != nil {
			logx.Errorw("audit log consumer stopped", logx.Field("err", err))
		}
	}()
	return nil
}

func initEsIndex(ctx context.Context, client *elasticsearch.Client) error {

	// 创建索引（适配v7客户端）
	existsResp, err := esapi.IndicesExistsRequest{
		Index: []string{biz.EsIndexName},
	}.Do(ctx, client)
	if err != nil {
		return fmt.Errorf("索引存在检查失败: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err = Body.Close()
	}(existsResp.Body)

	// 检查存在状态
	if existsResp.StatusCode == 404 { // 索引不存在
		createResp, err := esapi.IndicesCreateRequest{
			Index: biz.EsIndexName,
			Body:  strings.NewReader(es.Mapping),
		}.Do(ctx, client)
		if err != nil {
			return fmt.Errorf("创建索引请求失败: %w", err)
		}
		defer func(Body io.ReadCloser) {
			err = Body.Close()
		}(createResp.Body)
		// 解析响应
		if createResp.IsError() {
			return fmt.Errorf("ES返回错误: %s", createResp.String())
		}
	}
	return nil

}
