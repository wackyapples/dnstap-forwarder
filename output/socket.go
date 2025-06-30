package output

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

// SocketWriter handles writing events to a Unix domain socket
type SocketWriter struct {
	BaseWriter
	conn net.Conn
}

// NewSocketWriter creates a new socket writer
func NewSocketWriter(config config.Config, hostname string) (*SocketWriter, error) {
	sw := &SocketWriter{
		BaseWriter: BaseWriter{
			config:   config,
			buffer:   make([]*events.DNSEvent, 0, config.BufferSize),
			hostname: hostname,
		},
	}

	if err := sw.connect(); err != nil {
		return nil, err
	}

	return sw, nil
}

// connect establishes a connection to the Unix domain socket
func (sw *SocketWriter) connect() error {
	// Remove existing socket if it exists
	if _, err := os.Stat(sw.config.OutputPath); err == nil {
		if err := os.Remove(sw.config.OutputPath); err != nil {
			return fmt.Errorf("removing existing socket: %w", err)
		}
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", sw.config.OutputPath)
	if err != nil {
		return fmt.Errorf("creating socket listener: %w", err)
	}

	log.Printf("Created Unix domain socket: %s", sw.config.OutputPath)
	log.Printf("Waiting for syslog-ng/rsyslog to connect...")

	// Accept connection (this will block until a client connects)
	conn, err := listener.Accept()
	if err != nil {
		listener.Close()
		return fmt.Errorf("accepting socket connection: %w", err)
	}

	// Close the listener as we only need one connection
	listener.Close()

	sw.conn = conn
	sw.writer = bufio.NewWriterSize(conn, 64*1024) // 64KB buffer

	log.Printf("Socket connected and ready for writing")
	return nil
}

// AddEvent adds an event to the buffer
func (sw *SocketWriter) AddEvent(event *events.DNSEvent) error {
	sw.buffer = append(sw.buffer, event)

	if len(sw.buffer) >= sw.config.BatchSize {
		return sw.Flush()
	}
	return nil
}

// Flush writes all buffered events to the socket
func (sw *SocketWriter) Flush() error {
	if len(sw.buffer) == 0 {
		return nil
	}

	for _, event := range sw.buffer {
		message := createMessage(event, sw.hostname, sw.config.OutputFormat)
		if err := sw.writeMessage(message, sw.reconnect); err != nil {
			return err
		}
	}

	if err := sw.writer.Flush(); err != nil {
		return fmt.Errorf("flushing buffer: %w", err)
	}

	sw.eventCount += int64(len(sw.buffer))
	if sw.config.LogLevel == "debug" || sw.eventCount%1000 == 0 {
		log.Printf("Wrote %d events to socket (total: %d)", len(sw.buffer), sw.eventCount)
	}

	sw.buffer = sw.buffer[:0] // Clear the buffer
	return nil
}

// reconnect attempts to reconnect to the socket
func (sw *SocketWriter) reconnect() error {
	log.Printf("Attempting to reconnect to socket...")

	if sw.conn != nil {
		sw.conn.Close()
	}

	return sw.connect()
}

// Close closes the socket writer
func (sw *SocketWriter) Close() error {
	if err := sw.Flush(); err != nil {
		log.Printf("Error flushing on close: %v", err)
	}

	if sw.conn != nil {
		sw.conn.Close()
	}

	// Clean up the socket file
	if err := os.Remove(sw.config.OutputPath); err != nil {
		log.Printf("Error removing socket on close: %v", err)
	}

	return nil
}
