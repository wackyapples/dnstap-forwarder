package output

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

// testPipeClient connects to a named pipe and reads data
func testPipeClient(pipePath string, done chan<- bool) {
	// Wait a bit for the pipe to be created
	time.Sleep(100 * time.Millisecond)

	// Open the pipe for reading
	pipe, err := os.OpenFile(pipePath, os.O_RDONLY, 0644)
	if err != nil {
		fmt.Printf("Failed to open pipe for reading: %v\n", err)
		done <- false
		return
	}
	defer pipe.Close()

	// Read data from the pipe
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		// Just consume the data
		_ = scanner.Text()
	}

	done <- true
}

// testSocketClient connects to a Unix socket and reads data
func testSocketClient(socketPath string, done chan<- bool) {
	// Wait a bit for the socket to be created
	time.Sleep(100 * time.Millisecond)

	// Connect to the Unix socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Printf("Failed to connect to socket: %v\n", err)
		done <- false
		return
	}
	defer conn.Close()

	// Read data from the socket
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		// Just consume the data
		_ = scanner.Text()
	}

	done <- true
}

func TestNewWriter(t *testing.T) {
	tests := []struct {
		name        string
		outputType  string
		outputPath  string
		expectError bool
	}{
		{
			name:        "pipe output type",
			outputType:  "pipe",
			outputPath:  "/tmp/test.pipe",
			expectError: false,
		},
		{
			name:        "socket output type",
			outputType:  "socket",
			outputPath:  "/tmp/test.sock",
			expectError: false,
		},
		{
			name:        "invalid output type",
			outputType:  "invalid",
			outputPath:  "/tmp/test.invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				OutputType: config.OutputType(tt.outputType),
				OutputPath: tt.outputPath,
				BufferSize: 10,
			}

			// Start test client in background for pipe/socket tests
			var clientDone chan bool
			switch tt.outputType {
			case "pipe":
				clientDone = make(chan bool, 1)
				go testPipeClient(tt.outputPath, clientDone)
			case "socket":
				clientDone = make(chan bool, 1)
				go testSocketClient(tt.outputPath, clientDone)
			}

			writer, err := NewWriter(cfg)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if writer == nil {
				t.Errorf("Expected writer but got nil")
				return
			}

			// Test writing an event
			if tt.outputType == "pipe" || tt.outputType == "socket" {
				event := &events.DNSEvent{
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					QueryName:   "test.example.com",
					QueryType:   "A",
					MessageType: "QUERY",
				}

				if err := writer.AddEvent(event); err != nil {
					t.Errorf("Failed to add event: %v", err)
				}

				if err := writer.Flush(); err != nil {
					t.Errorf("Failed to flush: %v", err)
				}
			}

			// Clean up
			writer.Close()

			// Wait for test client to finish (with timeout)
			if clientDone != nil {
				select {
				case success := <-clientDone:
					if !success {
						t.Errorf("Test client failed")
					}
				case <-time.After(2 * time.Second):
					t.Errorf("Test client timeout")
				}
			}
		})
	}
}

func TestCreateSyslogMessage(t *testing.T) {
	event := &events.DNSEvent{
		Timestamp:   "2023-01-01T12:00:00Z",
		QueryName:   "example.com",
		QueryType:   "A",
		MessageType: "RESPONSE",
	}

	hostname := "testhost"
	message := createSyslogMessage(event, hostname)

	if message == "" {
		t.Errorf("Expected non-empty syslog message")
	}

	// Check that it contains expected components
	if len(message) < 50 {
		t.Errorf("Message too short: %s", message)
	}
}

func TestCreateJSONMessage(t *testing.T) {
	event := &events.DNSEvent{
		Timestamp:   "2023-01-01T12:00:00Z",
		QueryName:   "example.com",
		QueryType:   "A",
		MessageType: "RESPONSE",
		SourceIP:    "192.168.1.100",
		ServerIP:    "8.8.8.8",
	}

	message := createJSONMessage(event)

	if message == "" {
		t.Errorf("Expected non-empty JSON message")
	}

	// Check that it ends with newline
	if !strings.HasSuffix(message, "\n") {
		t.Errorf("JSON message should end with newline: %s", message)
	}

	// Check that it contains JSON content (starts with { and ends with })
	jsonContent := strings.TrimSpace(message)
	if !strings.HasPrefix(jsonContent, "{") || !strings.HasSuffix(jsonContent, "}") {
		t.Errorf("Message should be valid JSON: %s", message)
	}

	// Check that it contains expected fields
	if !strings.Contains(jsonContent, `"timestamp":"2023-01-01T12:00:00Z"`) {
		t.Errorf("JSON should contain timestamp: %s", message)
	}
	if !strings.Contains(jsonContent, `"query_name":"example.com"`) {
		t.Errorf("JSON should contain query_name: %s", message)
	}
	if !strings.Contains(jsonContent, `"query_type":"A"`) {
		t.Errorf("JSON should contain query_type: %s", message)
	}
	if !strings.Contains(jsonContent, `"message_type":"RESPONSE"`) {
		t.Errorf("JSON should contain message_type: %s", message)
	}
}

func TestCreateMessage(t *testing.T) {
	event := &events.DNSEvent{
		Timestamp:   "2023-01-01T12:00:00Z",
		QueryName:   "example.com",
		QueryType:   "A",
		MessageType: "RESPONSE",
	}

	hostname := "testhost"

	// Test JSON format
	jsonMessage := createMessage(event, hostname, config.OutputFormatJSON)
	if !strings.HasSuffix(jsonMessage, "\n") {
		t.Errorf("JSON message should end with newline")
	}
	jsonContent := strings.TrimSpace(jsonMessage)
	if !strings.HasPrefix(jsonContent, "{") || !strings.HasSuffix(jsonContent, "}") {
		t.Errorf("JSON message should be valid JSON: %s", jsonMessage)
	}

	// Test syslog format
	syslogMessage := createMessage(event, hostname, config.OutputFormatSyslog)
	if !strings.Contains(syslogMessage, "dnstap[") {
		t.Errorf("Syslog message should contain process info: %s", syslogMessage)
	}
	if !strings.Contains(syslogMessage, hostname) {
		t.Errorf("Syslog message should contain hostname: %s", syslogMessage)
	}

	// Test default format (should be syslog)
	defaultMessage := createMessage(event, hostname, config.OutputFormat("invalid"))
	if !strings.Contains(defaultMessage, "dnstap[") {
		t.Errorf("Default message should be syslog format: %s", defaultMessage)
	}
}
