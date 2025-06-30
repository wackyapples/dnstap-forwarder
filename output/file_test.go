package output

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

func TestFileWriter(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dnstap-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test configuration
	config := config.Config{
		OutputType:   "file",
		OutputPath:   filepath.Join(tempDir, "test.log"),
		OutputFormat: config.OutputFormatSyslog, // Use syslog format for testing
		BufferSize:   10,
		MaxFileSize:  1024, // 1KB for testing
		MaxFiles:     3,
		LogLevel:     "debug",
	}

	// Create file writer
	writer, err := NewFileWriter(config, "test-host")
	if err != nil {
		t.Fatalf("Failed to create file writer: %v", err)
	}
	defer writer.Close()

	// Create test events
	event := &events.DNSEvent{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		QueryName:   "example.com",
		QueryType:   "A",
		SourceIP:    "192.168.1.1",
		MessageType: "RESOLVER_QUERY",
	}

	// Add events to trigger rotation
	for range 50 {
		if err := writer.AddEvent(event); err != nil {
			t.Fatalf("Failed to add event: %v", err)
		}
	}

	// Flush to ensure all events are written
	if err := writer.Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}

	// Check that files were created
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read temp dir: %v", err)
	}

	if len(files) == 0 {
		t.Error("No files were created")
	}

	// Check that we have log files
	logFileCount := 0
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".log" {
			logFileCount++
		}
	}

	if logFileCount == 0 {
		t.Error("No log files were created")
	}

	t.Logf("Created %d log files in %s", logFileCount, tempDir)
}
