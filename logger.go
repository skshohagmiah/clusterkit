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
		if len(args) > 0 {
			l.logger.Printf("[INFO] %s %s", msg, formatKeyValues(args...))
		} else {
			l.logger.Printf("[INFO] %s", msg)
		}
	}
}

func (l *DefaultLogger) Warn(msg string, args ...interface{}) {
	if l.level <= LogLevelWarn {
		if len(args) > 0 {
			l.logger.Printf("[WARN] %s %s", msg, formatKeyValues(args...))
		} else {
			l.logger.Printf("[WARN] %s", msg)
		}
	}
}

func (l *DefaultLogger) Error(msg string, args ...interface{}) {
	if l.level <= LogLevelError {
		if len(args) > 0 {
			l.logger.Printf("[ERROR] %s %s", msg, formatKeyValues(args...))
		} else {
			l.logger.Printf("[ERROR] %s", msg)
		}
	}
}

func (l *DefaultLogger) Debug(msg string, args ...interface{}) {
	if l.level <= LogLevelDebug {
		if len(args) > 0 {
			l.logger.Printf("[DEBUG] %s %s", msg, formatKeyValues(args...))
		} else {
			l.logger.Printf("[DEBUG] %s", msg)
		}
	}
}

// formatKeyValues formats key-value pairs for structured logging
// Expects alternating key-value pairs: key1, value1, key2, value2, ...
func formatKeyValues(args ...interface{}) string {
	if len(args) == 0 {
		return ""
	}

	// If odd number of args, last one is just appended
	pairs := make([]string, 0, (len(args)+1)/2)

	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			pairs = append(pairs, fmt.Sprintf("%v=%v", args[i], args[i+1]))
		} else {
			// Odd arg at the end
			pairs = append(pairs, fmt.Sprintf("%v", args[i]))
		}
	}

	return fmt.Sprintf("(%s)", joinStrings(pairs, ", "))
}

// joinStrings joins strings with a separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// NoOpLogger is a logger that does nothing (for testing)
type NoOpLogger struct{}

func (l *NoOpLogger) Info(msg string, args ...interface{})  {}
func (l *NoOpLogger) Warn(msg string, args ...interface{})  {}
func (l *NoOpLogger) Error(msg string, args ...interface{}) {}
func (l *NoOpLogger) Debug(msg string, args ...interface{}) {}
