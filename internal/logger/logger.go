package logger

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Log zerolog.Logger

func Init() error {
	logDir := "logs"

	// Ensure logs/ dir exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	writer := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "zapbot.json"),
		MaxSize:    20, // MB
		MaxBackups: 5,
		MaxAge:     14, // days
		Compress:   true,
	}

	Log = zerolog.New(writer).
		With().
		Timestamp().
		Caller().
		Logger()

	return nil
}
