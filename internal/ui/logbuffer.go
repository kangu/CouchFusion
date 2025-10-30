package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type LogLevel int

const (
	Info LogLevel = iota
	Success
	Warn
	Error
)

type LogEvent struct {
	Level LogLevel
	Text  string
}

type LogBuffer struct {
	capacity int
	events   []LogEvent
	mu       sync.Mutex
}

func NewLogBuffer(cap int) *LogBuffer {
	return &LogBuffer{capacity: cap}
}

func (lb *LogBuffer) Append(level LogLevel, format string, args ...any) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	event := LogEvent{
		Level: level,
		Text:  fmt.Sprintf(format, args...),
	}
	lb.events = append(lb.events, event)
	if len(lb.events) > lb.capacity {
		lb.events = lb.events[len(lb.events)-lb.capacity:]
	}
}

func (lb *LogBuffer) Infof(format string, args ...any) {
	lb.Append(Info, format, args...)
}

func (lb *LogBuffer) Successf(format string, args ...any) {
	lb.Append(Success, format, args...)
}

func (lb *LogBuffer) Warnf(format string, args ...any) {
	lb.Append(Warn, format, args...)
}

func (lb *LogBuffer) Errorf(format string, args ...any) {
	lb.Append(Error, format, args...)
}

func (lb *LogBuffer) Render() string {
	lb.mu.Lock()
	events := make([]LogEvent, len(lb.events))
	copy(events, lb.events)
	lb.mu.Unlock()

	lines := make([]string, 0, len(events))
	for _, ev := range events {
		var styled string
		switch ev.Level {
		case Info:
			styled = LogInfo.Render(ev.Text)
		case Success:
			styled = LogSuccess.Render(ev.Text)
		case Warn:
			styled = LogWarn.Render(ev.Text)
		case Error:
			styled = LogError.Render(ev.Text)
		default:
			styled = ev.Text
		}
		lines = append(lines, styled)
	}
	if len(lines) == 0 {
		lines = append(lines, Hint.Render("No log output yet."))
	}
	return strings.Join(lines, "\n")
}

// Writer returns an io.Writer that appends data to the buffer using the given level.
func (lb *LogBuffer) Writer(level LogLevel) io.Writer {
	return &logBufferWriter{buffer: lb, level: level}
}

type logBufferWriter struct {
	buffer *LogBuffer
	level  LogLevel
}

func (w *logBufferWriter) Write(p []byte) (int, error) {
	text := string(p)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		w.buffer.Append(w.level, "%s", trimmed)
	}
	return len(p), nil
}
