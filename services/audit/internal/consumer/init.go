package consumer

import (
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/audit/internal/consumer/registry"
)

// 初始化所有消费者
func Init(c config.Config) error {
	for _, initializer := range registry.List() {
		if err := initializer.Run(c); err != nil {
			return err
		}
	}
	return nil
}
