package config

import (
	"fmt"
)

type MysqlConfig struct {
	DataSource string
}

type RabbitMQConfig struct {
	Host  string
	Port  int
	User  string
	Pass  string
	VHost string
}

type KafkaConfig struct {
	Brokers    []string
	Group      string
	Topic      string
	Offset     string `json:",options=first|last,default=last"`
	Conns      int    `json:",default=1"`        //Conns 对应 kafka queue 数量, 默认只启动一个
	Consumers  int    `json:",default=8"`        //控制 goroutine 的数量，从 kafka 中获取信息写入进程内的 channel
	Processors int    `json:",default=8"`        //控制当前消费的并发 goroutine 数量
	MinBytes   int    `json:",default=10240"`    // 10K
	MaxBytes   int    `json:",default=10485760"` // 10M
	Username   string `json:",optional"`
	Password   string `json:",optional"`
}

type ElasticSearchConfig struct {
	Addr string
}
type GorseConfig struct {
	GorseAddr   string
	GorseApikey string
}

func (r *RabbitMQConfig) Dns() string {
	return fmt.Sprintf(
		"amqp://%s:%s@%s:%d/%s",
		r.User,
		r.Pass,
		r.Host,
		r.Port,
		r.VHost,
	)
}
