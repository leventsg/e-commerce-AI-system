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
	Offset     string `json:"offset"`
	Conns      int    `json:"conns"`      // Conns 对应 kafka queue 数量, 默认只启动一个
	Consumers  int    `json:"consumers"`  // 控制 goroutine 的数量，从 kafka 中获取信息写入进程内的 channel
	Processors int    `json:"processors"` // 控制当前消费的并发 goroutine 数量
	MinBytes   int    `json:"min_bytes"`  // 10K
	MaxBytes   int    `json:"max_bytes"`  // 10M
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
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
