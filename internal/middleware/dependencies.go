package middleware

import (
	"github.com/Chandra179/gosdk/logger"
)

type Dependencies struct {
	logger logger.Logger
}

func NewDependencies(logger logger.Logger) *Dependencies {
	return &Dependencies{logger: logger}
}
