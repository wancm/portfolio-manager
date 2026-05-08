package shared

import (
	"encoding/json"
	"log/slog"
	"os"
	"time"
)

var AppLogger = newLogger()

func newLogger() *slog.Logger {
	opts := handlerOpts()
	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// UnixToTime 将 Unix 秒转换为 time.Time
func UnixToTime(unixSec int64) time.Time {
	return time.Unix(unixSec, 0).UTC()
}

// ToJsonIndent 将任意结构体序列化为带缩进的 JSON 字符串，便于日志记录
func ToJsonIndent(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
