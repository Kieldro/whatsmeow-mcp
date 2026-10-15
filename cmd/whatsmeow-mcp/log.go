package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// stderrLogger is a waLog.Logger that never touches stdout, which carries
// the MCP protocol.
type stderrLogger struct {
	w   io.Writer
	mu  *sync.Mutex
	mod string
	min int
}

var levels = map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}

func newLogger(w io.Writer, module, minLevel string) waLog.Logger {
	min, ok := levels[strings.ToUpper(minLevel)]
	if !ok {
		min = levels["INFO"]
	}
	return &stderrLogger{w: w, mu: &sync.Mutex{}, mod: module, min: min}
}

func (l *stderrLogger) logf(level, msg string, args ...any) {
	if levels[level] < l.min {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s [%s %s] %s\n", time.Now().Format("15:04:05.000"), l.mod, level, fmt.Sprintf(msg, args...))
}

func (l *stderrLogger) Errorf(msg string, args ...any) { l.logf("ERROR", msg, args...) }
func (l *stderrLogger) Warnf(msg string, args ...any)  { l.logf("WARN", msg, args...) }
func (l *stderrLogger) Infof(msg string, args ...any)  { l.logf("INFO", msg, args...) }
func (l *stderrLogger) Debugf(msg string, args ...any) { l.logf("DEBUG", msg, args...) }
func (l *stderrLogger) Sub(mod string) waLog.Logger {
	return &stderrLogger{w: l.w, mu: l.mu, mod: l.mod + "/" + mod, min: l.min}
}
