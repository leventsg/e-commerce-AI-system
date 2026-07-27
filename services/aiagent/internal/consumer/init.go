package consumer

import "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"

func Init(c config.Config) error {
	for _, initializer := range List() {
		if err := initializer.Run(c); err != nil {
			return err
		}
	}
	return nil
}
