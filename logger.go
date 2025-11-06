package clusterkit

import (
	"fmt"
	"log"
	"os"
)

// Logger interface for structured logging
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// DefaultLogger implements Logger using standard library
type DefaultLogger struct {
	logger *log.Logger
	level  LogLevel
}

// LogLevel represents logging levels
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// NewDefaultLogger creates a new default logger
func NewDefaultLogger(level LogLevel) *DefaultLogger {
	return &DefaultLogger{
		logger: log.New(os.Stdout, "[ClusterKit] ", log.LstdFlags|log.Lmsgprefix),
		level:  level,
	}
}

func (l *DefaultLogger) Info(msg string, args ...interface{}) {
	if l.level <= LogLevelInfo {
		l.logger.Printf("[INFO] "+msg, args...)
	}
}

func (l *DefaultLogger) Warn(msg string, args ...interface{}) {
	if l.level <= LogLevelWarn {
		l.logger.Printf("[WARN] "+msg, args...)
	}
}

func (l *DefaultLogger) Error(msg string, args ...interface{}) {
	if l.level <= LogLevelError {
		l.logger.Printf("[ERROR] "+msg, args...)
	}
}

func (l *DefaultLogger) Debug(msg string, args ...interface{}) {
	if l.level <= LogLevelDebug {
		l.logger.Printf("[DEBUG] "+msg, args...)
	}
}

// NoOpLogger is a logger that does nothing (for testing)
type NoOpLogger struct{}

func (l *NoOpLogger) Info(msg string, args ...interface{})  {}
func (l *NoOpLogger) Warn(msg string, args ...interface{})  {}
func (l *NoOpLogger) Error(msg string, args ...interface{}) {}
func (l *NoOpLogger) Debug(msg string, args ...interface{}) {}

// Helper function for backward compatibility - logs to stdout
func logInfo(msg string, args ...interface{}) {
	fmt.Printf("[INFO] "+msg+"\n", args...)
}

func logWarn(msg string, args ...interface{}) {
	fmt.Printf("[WARN] "+msg+"\n", args...)
}

func logError(msg string, args ...interface{}) {
	fmt.Printf("[ERROR] "+msg+"\n", args...)
}

func logDebug(msg string, args ...interface{}) {
	fmt.Printf("[DEBUG] "+msg+"\n", args...)
}
