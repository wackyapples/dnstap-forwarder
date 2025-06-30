package output

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

// Writer interface defines the contract for output writers
type Writer interface {
	AddEvent(event *events.DNSEvent) error
	Flush() error
	Close() error
}

// BaseWriter provides common functionality for all output types
type BaseWriter struct {
	config     config.Config
	writer     *bufio.Writer
	buffer     []*events.DNSEvent
	hostname   string
	eventCount int64
}

// CircuitBreaker implements a simple circuit breaker pattern
type CircuitBreaker struct {
	failureThreshold int32
	failureCount     int32
	lastFailureTime  time.Time
	timeout          time.Duration
	state            int32 // 0=closed, 1=open, 2=half-open
	mutex            sync.RWMutex
}

func NewCircuitBreaker(failureThreshold int32, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		failureThreshold: failureThreshold,
		timeout:          timeout,
	}
}

func (cb *CircuitBreaker) CanExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	if atomic.LoadInt32(&cb.state) == 0 { // closed
		return true
	}

	if atomic.LoadInt32(&cb.state) == 1 { // open
		if time.Since(cb.lastFailureTime) > cb.timeout {
			atomic.StoreInt32(&cb.state, 2) // half-open
			return true
		}
		return false
	}

	return true // half-open
}

func (cb *CircuitBreaker) OnSuccess() {
	atomic.StoreInt32(&cb.state, 0) // closed
	atomic.StoreInt32(&cb.failureCount, 0)
}

func (cb *CircuitBreaker) OnFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.lastFailureTime = time.Now()
	newCount := atomic.AddInt32(&cb.failureCount, 1)

	if newCount >= cb.failureThreshold {
		atomic.StoreInt32(&cb.state, 1) // open
	}
}

// NewWriter creates a new output writer based on the configuration
func NewWriter(config config.Config) (Writer, error) {
	hostname, _ := os.Hostname()

	switch config.OutputType {
	case "pipe":
		return NewPipeWriter(config, hostname)
	case "socket":
		return NewSocketWriter(config, hostname)
	case "file":
		return NewFileWriter(config, hostname)
	case "hec":
		return NewHECWriter(config, hostname)
	default:
		return nil, fmt.Errorf("unsupported output type: %s", config.OutputType)
	}
}

// createSyslogMessage creates a syslog-style message with JSON payload
func createSyslogMessage(event *events.DNSEvent, hostname string) string {
	return fmt.Sprintf("<%d>%s %s dnstap[%d]: %s\n",
		16+6, // facility 16 (local0), severity 6 (info)
		time.Now().Format("Jan 02 15:04:05"),
		hostname,
		os.Getpid(),
		event.ToJSON())
}

// createJSONMessage creates a pure JSON message
func createJSONMessage(event *events.DNSEvent) string {
	return event.ToJSON() + "\n"
}

// createMessage creates a message based on the configured output format
func createMessage(event *events.DNSEvent, hostname string, format config.OutputFormat) string {
	switch format {
	case config.OutputFormatJSON:
		return createJSONMessage(event)
	case config.OutputFormatSyslog:
		return createSyslogMessage(event, hostname)
	default:
		// Default to syslog format for unsupported or empty formats
		return createSyslogMessage(event, hostname)
	}
}

// writeMessage writes a message to the buffered writer with reconnection logic
func (bw *BaseWriter) writeMessage(message string, reconnect func() error) error {
	if _, err := bw.writer.WriteString(message); err != nil {
		// If write fails, try to reconnect
		log.Printf("Write error, attempting to reconnect: %v", err)
		if err := reconnect(); err != nil {
			return fmt.Errorf("failed to reconnect: %w", err)
		}
		// Retry the write
		if _, err := bw.writer.WriteString(message); err != nil {
			return fmt.Errorf("retry write failed: %w", err)
		}
	}
	return nil
}

// flushBatchBuffer writes all buffered events as a batch (JSON array if in JSON mode and batch size > 1)
func (bw *BaseWriter) flushBatchBuffer() error {
	if len(bw.buffer) == 0 {
		return nil
	}

	if bw.config.OutputFormat == config.OutputFormatJSON && bw.config.BatchSize > 1 && len(bw.buffer) > 1 {
		// Output as a JSON array
		jsonArray := "["
		for i, event := range bw.buffer {
			if i > 0 {
				jsonArray += ","
			}
			jsonArray += event.ToJSON()
		}
		jsonArray += "]\n"
		if err := bw.writeMessage(jsonArray, func() error { return nil }); err != nil {
			return err
		}
	} else {
		// Output each event individually
		for _, event := range bw.buffer {
			message := createMessage(event, bw.hostname, bw.config.OutputFormat)
			if err := bw.writeMessage(message, func() error { return nil }); err != nil {
				return err
			}
		}
	}

	if err := bw.writer.Flush(); err != nil {
		return fmt.Errorf("flushing buffer: %w", err)
	}

	bw.eventCount += int64(len(bw.buffer))
	if bw.config.LogLevel == "debug" || bw.eventCount%1000 == 0 {
		log.Printf("Wrote %d events to output (total: %d)", len(bw.buffer), bw.eventCount)
	}

	bw.buffer = bw.buffer[:0] // Clear the buffer
	return nil
}

// flushBuffer writes all buffered events to the output (legacy, now calls flushBatchBuffer)
func (bw *BaseWriter) flushBuffer() error {
	return bw.flushBatchBuffer()
}
