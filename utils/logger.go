package utils

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// A bot logger used to log everything
// It implements the interfaces for both tgbot and badger
type BotLogger struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewBotLogger(w io.Writer) *BotLogger {
	return &BotLogger{writer: w}
}

// Interfaces for badger
func (l *BotLogger) log(level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006/01/02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.writer, "[%s] %s: %s\n", timestamp, level, msg)
}

func (l *BotLogger) Debugf(format string, args ...interface{}) {
	l.log("DEBUG", format, args...)
}

func (l *BotLogger) Infof(format string, args ...interface{}) {
	l.log("INFO", format, args...)
}

func (l *BotLogger) Warningf(format string, args ...interface{}) {
	l.log("WARNING", format, args...)
}

func (l *BotLogger) Errorf(format string, args ...interface{}) {
	l.log("ERROR", format, args...)
}

// additional methods
func (l *BotLogger) Debug(args ...interface{}) {
	l.Debugf("%s", fmt.Sprint(args...))
}

func (l *BotLogger) Info(args ...interface{}) {
	l.Infof("%s", fmt.Sprint(args...))
}

func (l *BotLogger) Warning(args ...interface{}) {
	l.Warningf("%s", fmt.Sprint(args...))
}

func (l *BotLogger) Error(args ...interface{}) {
	l.Errorf("%s", fmt.Sprint(args...))
}

func (l *BotLogger) Fatal(args ...interface{}) {
	l.Errorf("%s", fmt.Sprint(args...))
	os.Exit(1)
}

func (l *BotLogger) Fatalf(format string, args ...interface{}) {
	l.Errorf(format, args...)
	os.Exit(1)
}

// Interfaces for tgbot
func (l *BotLogger) Println(v ...interface{}) {
	l.Info(v...)
}

func (l *BotLogger) Printf(format string, v ...interface{}) {
	l.Infof(format, v...)
}
