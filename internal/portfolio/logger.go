// internal/portfolio/logger.go
package portfolio

import (
	"log/slog"
	"os"
)

func NewDefaultLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}
