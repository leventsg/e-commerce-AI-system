package config

import (
	"errors"
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
	Brokers  []string
	Username string `json:",optional"`
	Password string `json:",optional"`
	Topics   map[string]KafkaTopicConfig
}

type KafkaTopicConfig struct {
	Brokers     []string `json:",optional"`
	Username    string   `json:",optional"`
	Password    string   `json:",optional"`
	Topic       string
	Group       string
	Offset      string `json:",options=first|last,default=last"`
	Conns       int    `json:",default=1"`        // Conns 对应 kafka queue 数量, 默认只启动一个
	Consumers   int    `json:",default=8"`        // 控制 goroutine 的数量，从 kafka 中获取信息写入进程内的 channel
	Processors  int    `json:",default=8"`        // 控制当前消费的并发 goroutine 数量
	MinBytes    int    `json:",default=10240"`    // 10K
	MaxBytes    int    `json:",default=10485760"` // 10M
	ForceCommit bool   `json:",default=true"`     // 是否强制提交，默认 true
}

func (c KafkaConfig) TopicConfig(name string) (KafkaTopicConfig, error) {
	if len(c.Brokers) == 0 {
		return KafkaTopicConfig{}, errors.New("kafka brokers is empty")
	}
	topic, ok := c.Topics[name]
	if !ok {
		return KafkaTopicConfig{}, fmt.Errorf("kafka topic config %q not found", name)
	}
	topic.Brokers = append([]string(nil), c.Brokers...)
	topic.Username = c.Username
	topic.Password = c.Password
	return topic, nil
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
