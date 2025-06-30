package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
	"dnstap-forwarder/output"
)

func TestHECRaceConditions(t *testing.T) {
	fmt.Println("Testing race condition fixes in HEC Writer...")

	// Create test configuration
	cfg := config.Config{
		OutputType:        "hec",
		HECURL:            "http://localhost:8088/services/collector",
		HECToken:          "test-token",
		BufferSize:        100,
		BatchSize:         10,
		FlushInterval:     1 * time.Second,
		LogLevel:          "info",
		HECMaxRetries:     3,
		HECRetryDelay:     100 * time.Millisecond,
		HECCircuitBreaker: true,
	}

	// Create HEC writer
	writer, err := output.NewHECWriter(cfg, "test-host")
	if err != nil {
		t.Fatalf("Failed to create HEC writer: %v", err)
	}

	// Test concurrent AddEvent operations
	testConcurrentAddEvents(t, writer)
	// Test concurrent Flush operations
	testConcurrentFlush(t, writer)
	// Test concurrent stats access
	testConcurrentStats(t, writer)
	// Test mixed operations
	testMixedOperations(t, writer)
}

func testConcurrentAddEvents(t *testing.T, writer *output.HECWriter) {
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := &events.DNSEvent{
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					QueryName:   fmt.Sprintf("test%d.example.com", j),
					QueryType:   "A",
					MessageType: "QUERY",
					SourceIP:    fmt.Sprintf("192.168.1.%d", j%255),
				}
				if err := writer.AddEvent(event); err != nil {
					t.Errorf("Error adding event: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentFlush(t *testing.T, writer *output.HECWriter) {
	var wg sync.WaitGroup
	numGoroutines := 5

	// First add some events
	for i := 0; i < 50; i++ {
		event := &events.DNSEvent{
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			QueryName:   fmt.Sprintf("flush-test%d.example.com", i),
			QueryType:   "A",
			MessageType: "QUERY",
			SourceIP:    fmt.Sprintf("10.0.0.%d", i%255),
		}
		writer.AddEvent(event)
	}

	// Now test concurrent flush operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := writer.Flush(); err != nil {
					t.Errorf("Error flushing: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}

func testConcurrentStats(t *testing.T, writer *output.HECWriter) {
	var wg sync.WaitGroup
	numGoroutines := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				stats := writer.GetStats()
				if stats == nil {
					t.Errorf("Error getting stats")
				}
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}

func testMixedOperations(t *testing.T, writer *output.HECWriter) {
	var wg sync.WaitGroup
	numGoroutines := 15

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				// Add events
				event := &events.DNSEvent{
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					QueryName:   fmt.Sprintf("mixed-test%d.example.com", j),
					QueryType:   "A",
					MessageType: "QUERY",
					SourceIP:    fmt.Sprintf("172.16.0.%d", j%255),
				}
				writer.AddEvent(event)

				// Get stats
				stats := writer.GetStats()
				if stats == nil {
					t.Errorf("Error getting stats")
				}

				// Flush occasionally
				if j%5 == 0 {
					writer.Flush()
				}

				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
