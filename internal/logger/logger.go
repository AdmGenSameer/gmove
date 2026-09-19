package logger

import (
	"fmt"
	"sync"
	"time"

	"github.com/AdmGenSameer/gmove/internal/database"
)

type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

var (
	defaultLogger *Logger
	mu            sync.RWMutex
)

// Logger manages buffered asynchronous logging to SQLite.
type Logger struct {
	repo      *database.Repository
	eventChan chan database.EventRecord
	flushChan chan chan struct{}
	doneChan  chan struct{}
	wg        sync.WaitGroup
	closed    bool
	closeMu   sync.Mutex
}

// Init initializes the global logger with the SQLite repository.
func Init(repo *database.Repository) *Logger {
	mu.Lock()
	defer mu.Unlock()

	if defaultLogger != nil {
		defaultLogger.Close()
	}

	l := &Logger{
		repo:      repo,
		eventChan: make(chan database.EventRecord, 1000),
		flushChan: make(chan chan struct{}),
		doneChan:  make(chan struct{}),
	}

	l.wg.Add(1)
	go l.worker()

	defaultLogger = l
	return l
}

// Get returns the global logger instance.
func Get() *Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}

// Close flushes any pending events and shuts down the worker goroutine.
func Close() {
	mu.Lock()
	l := defaultLogger
	defaultLogger = nil
	mu.Unlock()

	if l != nil {
		l.Close()
	}
}

// Flush ensures all queued logs are persisted to SQLite.
func Flush() {
	l := Get()
	if l != nil {
		l.Flush()
	}
}

func (l *Logger) worker() {
	defer l.wg.Done()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var batch []database.EventRecord

	flush := func() {
		if len(batch) > 0 && l.repo != nil {
			_ = l.repo.LogEventBatch(batch)
			batch = nil
		}
	}

	for {
		select {
		case ev, ok := <-l.eventChan:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= 25 {
				flush()
			}

		case <-ticker.C:
			flush()

		case ack := <-l.flushChan:
			for {
				select {
				case ev := <-l.eventChan:
					batch = append(batch, ev)
				default:
					goto drained
				}
			}
		drained:
			flush()
			close(ack)

		case <-l.doneChan:
			// Drain remaining events in channel
			for {
				select {
				case ev, ok := <-l.eventChan:
					if ok {
						batch = append(batch, ev)
					} else {
						flush()
						return
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (l *Logger) Close() {
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return
	}
	l.closed = true
	l.closeMu.Unlock()

	close(l.doneChan)
	l.wg.Wait()
}

func (l *Logger) Flush() {
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return
	}
	l.closeMu.Unlock()

	ack := make(chan struct{})
	select {
	case l.flushChan <- ack:
		<-ack
	case <-time.After(1 * time.Second):
	}
}

func (l *Logger) Log(level Level, component string, opID *int64, message, details string) {
	if l == nil {
		return
	}
	l.closeMu.Lock()
	if l.closed {
		l.closeMu.Unlock()
		return
	}
	l.closeMu.Unlock()

	rec := database.EventRecord{
		OperationID: opID,
		Timestamp:   time.Now().UTC(),
		Level:       string(level),
		Component:   component,
		Message:     message,
		Details:     details,
	}

	select {
	case l.eventChan <- rec:
	default:
		// Channel full, write directly to avoid dropping critical logs
		if l.repo != nil {
			_ = l.repo.LogEvent(opID, string(level), component, message, details)
		}
	}
}

// ScopedLogger provides a convenient builder for logging within a subsystem and operation.
type ScopedLogger struct {
	component string
	opID      *int64
}

func For(component string) *ScopedLogger {
	return &ScopedLogger{component: component}
}

func (s *ScopedLogger) WithOp(opID int64) *ScopedLogger {
	id := opID
	return &ScopedLogger{
		component: s.component,
		opID:      &id,
	}
}

func (s *ScopedLogger) Debug(msg string, details ...string) {
	d := ""
	if len(details) > 0 {
		d = details[0]
	}
	Log(LevelDebug, s.component, s.opID, msg, d)
}

func (s *ScopedLogger) Info(msg string, details ...string) {
	d := ""
	if len(details) > 0 {
		d = details[0]
	}
	Log(LevelInfo, s.component, s.opID, msg, d)
}

func (s *ScopedLogger) Warn(msg string, details ...string) {
	d := ""
	if len(details) > 0 {
		d = details[0]
	}
	Log(LevelWarn, s.component, s.opID, msg, d)
}

func (s *ScopedLogger) Error(msg string, details ...string) {
	d := ""
	if len(details) > 0 {
		d = details[0]
	}
	Log(LevelError, s.component, s.opID, msg, d)
}

func (s *ScopedLogger) Debugf(format string, args ...any) {
	Log(LevelDebug, s.component, s.opID, fmt.Sprintf(format, args...), "")
}

func (s *ScopedLogger) Infof(format string, args ...any) {
	Log(LevelInfo, s.component, s.opID, fmt.Sprintf(format, args...), "")
}

func (s *ScopedLogger) Warnf(format string, args ...any) {
	Log(LevelWarn, s.component, s.opID, fmt.Sprintf(format, args...), "")
}

func (s *ScopedLogger) Errorf(format string, args ...any) {
	Log(LevelError, s.component, s.opID, fmt.Sprintf(format, args...), "")
}

// Global package-level functions
func Log(level Level, component string, opID *int64, message, details string) {
	l := Get()
	if l != nil {
		l.Log(level, component, opID, message, details)
	}
}

func Debug(component, msg string, details ...string) {
	For(component).Debug(msg, details...)
}

func Info(component, msg string, details ...string) {
	For(component).Info(msg, details...)
}

func Warn(component, msg string, details ...string) {
	For(component).Warn(msg, details...)
}

func Error(component, msg string, details ...string) {
	For(component).Error(msg, details...)
}

func Debugf(component, format string, args ...any) {
	For(component).Debugf(format, args...)
}

func Infof(component, format string, args ...any) {
	For(component).Infof(format, args...)
}

func Warnf(component, format string, args ...any) {
	For(component).Warnf(format, args...)
}

func Errorf(component, format string, args ...any) {
	For(component).Errorf(format, args...)
}
