package output

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"syscall"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

// PipeWriter handles writing events to a named pipe
type PipeWriter struct {
	BaseWriter
	pipeFile *os.File
}

// NewPipeWriter creates a new pipe writer
func NewPipeWriter(config config.Config, hostname string) (*PipeWriter, error) {
	pw := &PipeWriter{
		BaseWriter: BaseWriter{
			config:   config,
			buffer:   make([]*events.DNSEvent, 0, config.BufferSize),
			hostname: hostname,
		},
	}

	if err := pw.openPipe(); err != nil {
		return nil, err
	}

	return pw, nil
}

// openPipe creates and opens the named pipe
func (pw *PipeWriter) openPipe() error {
	// Remove existing pipe if it exists
	if _, err := os.Stat(pw.config.OutputPath); err == nil {
		if err := os.Remove(pw.config.OutputPath); err != nil {
			return fmt.Errorf("removing existing pipe: %w", err)
		}
	}

	// Create named pipe (FIFO)
	if err := syscall.Mkfifo(pw.config.OutputPath, 0644); err != nil {
		return fmt.Errorf("creating named pipe: %w", err)
	}

	log.Printf("Created named pipe: %s", pw.config.OutputPath)
	log.Printf("Waiting for syslog-ng/rsyslog to connect...")

	// Open pipe for writing (this will block until a reader connects)
	pipeFile, err := os.OpenFile(pw.config.OutputPath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening pipe for writing: %w", err)
	}

	pw.pipeFile = pipeFile
	pw.writer = bufio.NewWriterSize(pipeFile, 64*1024) // 64KB buffer

	log.Printf("Pipe connected and ready for writing")
	return nil
}

// AddEvent adds an event to the buffer
func (pw *PipeWriter) AddEvent(event *events.DNSEvent) error {
	pw.buffer = append(pw.buffer, event)

	if len(pw.buffer) >= pw.config.BatchSize {
		return pw.Flush()
	}
	return nil
}

// Flush writes all buffered events to the pipe
func (pw *PipeWriter) Flush() error {
	if len(pw.buffer) == 0 {
		return nil
	}

	for _, event := range pw.buffer {
		message := createMessage(event, pw.hostname, pw.config.OutputFormat)
		if err := pw.writeMessage(message, pw.reconnect); err != nil {
			return err
		}
	}

	if err := pw.writer.Flush(); err != nil {
		return fmt.Errorf("flushing buffer: %w", err)
	}

	pw.eventCount += int64(len(pw.buffer))
	if pw.config.LogLevel == "debug" || pw.eventCount%1000 == 0 {
		log.Printf("Wrote %d events to pipe (total: %d)", len(pw.buffer), pw.eventCount)
	}

	pw.buffer = pw.buffer[:0] // Clear the buffer
	return nil
}

// reconnect attempts to reconnect to the pipe
func (pw *PipeWriter) reconnect() error {
	log.Printf("Attempting to reconnect to pipe...")

	if pw.pipeFile != nil {
		pw.pipeFile.Close()
	}

	return pw.openPipe()
}

// Close closes the pipe writer
func (pw *PipeWriter) Close() error {
	if err := pw.Flush(); err != nil {
		log.Printf("Error flushing on close: %v", err)
	}

	if pw.pipeFile != nil {
		pw.pipeFile.Close()
	}

	// Clean up the named pipe
	if err := os.Remove(pw.config.OutputPath); err != nil {
		log.Printf("Error removing pipe on close: %v", err)
	}

	return nil
}
