package logging

import (
	"fmt"
	"log"
)

var (
	warnLogger  = log.New(log.Writer(), "[WARN] ", log.LstdFlags)
	infoLogger  = log.New(log.Writer(), "[INFO] ", log.LstdFlags)
	errorLogger = log.New(log.Writer(), "[ERROR] ", log.LstdFlags)
	version     = ""
)

// SetVersion records the CLI version for use in log headers if desired.
func SetVersion(v string) {
	version = v
}

func prefix(msg string) string {
	if version == "" {
		return msg
	}
	return fmt.Sprintf("%s | %s", version, msg)
}

// Infof logs an informational message.
func Infof(format string, args ...any) {
	infoLogger.Printf(prefix(format), args...)
}

// Warnf logs a warning message.
func Warnf(format string, args ...any) {
	warnLogger.Printf(prefix(format), args...)
}

// Errorf logs an error message without exiting.
func Errorf(format string, args ...any) {
	errorLogger.Printf(prefix(format), args...)
}

// Fatalf logs an error message and exits.
func Fatalf(format string, args ...any) {
	errorLogger.Fatalf(prefix(format), args...)
}
