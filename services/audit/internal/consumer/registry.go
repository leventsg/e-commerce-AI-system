package consumer

import "github.com/leventsg/e-commerce-AI-system/services/audit/internal/config"

type Initializer struct {
	Name string
	Run  func(config.Config) error
}

var initializers []Initializer

func Register(name string, run func(config.Config) error) {
	initializers = append(initializers, Initializer{
		Name: name,
		Run:  run,
	})
}

func List() []Initializer {
	return append([]Initializer(nil), initializers...)
}
