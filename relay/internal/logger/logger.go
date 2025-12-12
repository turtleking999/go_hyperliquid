// Package logger provides a high-performance logging wrapper using zap.
package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// Log is the global logger instance.
	Log *zap.Logger
	// Sugar is the sugared logger for convenience.
	Sugar *zap.SugaredLogger
)

// Config holds logger configuration.
type Config struct {
	Level       string `mapstructure:"level"`        // debug, info, warn, error
	Development bool   `mapstructure:"development"`  // Use development mode
	Encoding    string `mapstructure:"encoding"`     // json or console
}

// Init initializes the global logger.
func Init(cfg *Config) error {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	var config zap.Config
	if cfg.Development {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
	}

	config.Level = zap.NewAtomicLevelAt(level)
	if cfg.Encoding != "" {
		config.Encoding = cfg.Encoding
	}

	var err error
	Log, err = config.Build(
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	)
	if err != nil {
		return err
	}

	Sugar = Log.Sugar()
	return nil
}

// InitDefault initializes with default settings based on environment.
func InitDefault() {
	env := os.Getenv("ENV")
	cfg := &Config{
		Level:       "info",
		Development: env != "production",
		Encoding:    "json",
	}
	if cfg.Development {
		cfg.Level = "debug"
		cfg.Encoding = "console"
	}
	if err := Init(cfg); err != nil {
		panic(err)
	}
}

// Debug logs a debug message.
func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, fields...)
}

// Info logs an info message.
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, fields...)
}

// Warn logs a warning message.
func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, fields...)
}

// Error logs an error message.
func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, fields...)
}

// Fatal logs a fatal message and exits.
func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, fields...)
}

// With creates a child logger with additional fields.
func With(fields ...zap.Field) *zap.Logger {
	return Log.With(fields...)
}

// Sync flushes any buffered log entries.
func Sync() error {
	return Log.Sync()
}
