package logger

import (
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Log zerolog.Logger

func Init() error {
	logDir := "logs"

	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		return path.Base(file) + ":" + strconv.Itoa(line)
	}

	// Ensure logs/ dir exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	writer := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "logs.json"),
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
