package consumer

import (
	"github.com/leventsg/e-commerce-AI-system/services/checkout/internal/config"
)

// 初始化所有消费者
func Init(c config.Config) error {
	for _, initializer := range List() {
		if err := initializer.Run(c); err != nil {
			return err
		}
	}
	return nil
}
