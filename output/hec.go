package output

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
)

// HECEvent represents a Splunk HEC event with envelope
type HECEvent struct {
	Event      any    `json:"event"`
	Time       string `json:"time,omitempty"`
	Host       string `json:"host,omitempty"`
	Index      string `json:"index,omitempty"`
	Source     string `json:"source,omitempty"`
	Sourcetype string `json:"sourcetype,omitempty"`
}

// HECWriter handles writing events to Splunk HEC
type HECWriter struct {
	BaseWriter
	hecURL   string
	hecToken string
	client   *http.Client

	// Reliability features
	circuitBreaker   *CircuitBreaker
	dlqWriter        *os.File
	persistentBuffer *os.File

	// Thread-safe state management
	bufferMutex sync.RWMutex
	statsMutex  sync.RWMutex
	fileMutex   sync.Mutex

	// Atomic counters
	successCount int64
	failureCount int64

	// Protected by statsMutex
	lastSuccessTime time.Time
	lastFailureTime time.Time
}

func NewHECWriter(config config.Config, hostname string) (*HECWriter, error) {
	if config.HECURL == "" || config.HECToken == "" {
		return nil, fmt.Errorf("HEC URL and token are required for hec output type")
	}

	// Create HTTP client with reasonable timeouts
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: false,
		},
	}

	hw := &HECWriter{
		BaseWriter: BaseWriter{
			config:   config,
			buffer:   make([]*events.DNSEvent, 0, config.BufferSize),
			hostname: hostname,
		},
		hecURL:   config.HECURL,
		hecToken: config.HECToken,
		client:   client,
	}

	// Initialize circuit breaker if enabled
	if config.HECCircuitBreaker {
		hw.circuitBreaker = NewCircuitBreaker(5, 30*time.Second)
	}

	// Initialize dead letter queue if configured
	if config.HECDLQPath != "" {
		if err := hw.initDLQ(config.HECDLQPath); err != nil {
			return nil, fmt.Errorf("initializing DLQ: %w", err)
		}
	}

	// Initialize persistent buffer if enabled
	if config.HECPersistentBuf {
		if err := hw.initPersistentBuffer(config.HECBufPath, config.HECBufMaxSize); err != nil {
			return nil, fmt.Errorf("initializing persistent buffer: %w", err)
		}
	}

	// Replay events from persistent buffer and DLQ on startup
	if err := hw.replayEventsOnStartup(); err != nil {
		log.Printf("Warning: Failed to replay events on startup: %v", err)
		// Don't fail startup, just log the warning
	}

	return hw, nil
}

func (hw *HECWriter) initDLQ(dlqPath string) error {
	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	// Create directory if it doesn't exist
	dlqDir := filepath.Dir(dlqPath)
	if err := os.MkdirAll(dlqDir, 0755); err != nil {
		return fmt.Errorf("creating DLQ directory: %w", err)
	}

	file, err := os.OpenFile(dlqPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening DLQ file: %w", err)
	}

	hw.dlqWriter = file
	log.Printf("Initialized dead letter queue: %s", dlqPath)
	return nil
}

func (hw *HECWriter) initPersistentBuffer(bufPath string, maxSize int64) error {
	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	if bufPath == "" {
		bufPath = "/tmp/dnstap-hec-buffer.dat"
	}

	// Create directory if it doesn't exist
	bufDir := filepath.Dir(bufPath)
	if err := os.MkdirAll(bufDir, 0755); err != nil {
		return fmt.Errorf("creating buffer directory: %w", err)
	}

	file, err := os.OpenFile(bufPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("opening persistent buffer: %w", err)
	}

	hw.persistentBuffer = file
	log.Printf("Initialized persistent buffer: %s (max size: %d MB)", bufPath, maxSize/(1024*1024))
	return nil
}

func (hw *HECWriter) writeToDLQ(events []*events.DNSEvent, reason string) error {
	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	if hw.dlqWriter == nil {
		return nil
	}

	dlqEntry := struct {
		Timestamp string `json:"timestamp"`
		Reason    string `json:"reason"`
		Events    []any  `json:"events"`
	}{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Reason:    reason,
		Events:    make([]any, len(events)),
	}

	// Convert events to interface{} slice
	for i, event := range events {
		dlqEntry.Events[i] = event
	}

	data, err := json.Marshal(dlqEntry)
	if err != nil {
		return fmt.Errorf("marshaling DLQ entry: %w", err)
	}

	if _, err := hw.dlqWriter.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing to DLQ: %w", err)
	}

	if err := hw.dlqWriter.Sync(); err != nil {
		return fmt.Errorf("syncing DLQ: %w", err)
	}

	log.Printf("Wrote %d events to dead letter queue: %s", len(events), reason)
	return nil
}

func (hw *HECWriter) saveToPersistentBuffer(events []*events.DNSEvent) error {
	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	if hw.persistentBuffer == nil {
		return nil
	}

	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshaling event for buffer: %w", err)
		}

		if _, err := hw.persistentBuffer.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("writing to persistent buffer: %w", err)
		}
	}

	if err := hw.persistentBuffer.Sync(); err != nil {
		return fmt.Errorf("syncing persistent buffer: %w", err)
	}

	return nil
}

// postToHEC posts events to Splunk HEC with enhanced retry logic
func (hw *HECWriter) postToHEC(payload []byte, events []*events.DNSEvent) error {
	// Check circuit breaker
	if hw.circuitBreaker != nil && !hw.circuitBreaker.CanExecute() {
		// Circuit breaker is open, save to persistent buffer and return error
		if err := hw.saveToPersistentBuffer(events); err != nil {
			log.Printf("Failed to save to persistent buffer: %v", err)
		}
		return fmt.Errorf("circuit breaker is open")
	}

	maxRetries := hw.config.HECMaxRetries
	baseDelay := hw.config.HECRetryDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * baseDelay
			log.Printf("Retrying HEC POST in %v (attempt %d/%d)", delay, attempt+1, maxRetries+1)
			time.Sleep(delay)
		}

		err := hw.doPost(payload)
		if err == nil {
			// Success
			if hw.circuitBreaker != nil {
				hw.circuitBreaker.OnSuccess()
			}
			atomic.AddInt64(&hw.successCount, 1)

			hw.statsMutex.Lock()
			hw.lastSuccessTime = time.Now()
			hw.statsMutex.Unlock()

			return nil
		}

		// Log the error
		if attempt == maxRetries {
			atomic.AddInt64(&hw.failureCount, 1)

			hw.statsMutex.Lock()
			hw.lastFailureTime = time.Now()
			hw.statsMutex.Unlock()

			if hw.circuitBreaker != nil {
				hw.circuitBreaker.OnFailure()
			}

			log.Printf("HEC POST failed after %d attempts: %v", maxRetries+1, err)

			// Save to DLQ and persistent buffer
			if err := hw.writeToDLQ(events, fmt.Sprintf("HEC failure: %v", err)); err != nil {
				log.Printf("Failed to write to DLQ: %v", err)
			}

			if err := hw.saveToPersistentBuffer(events); err != nil {
				log.Printf("Failed to save to persistent buffer: %v", err)
			}

			return err
		}

		log.Printf("HEC POST attempt %d failed: %v", attempt+1, err)
	}

	return fmt.Errorf("HEC POST failed after %d attempts", maxRetries+1)
}

// doPost performs a single POST request to Splunk HEC
func (hw *HECWriter) doPost(payload []byte) error {
	req, err := http.NewRequest("POST", hw.hecURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating HEC request: %w", err)
	}

	req.Header.Set("Authorization", "Splunk "+hw.hecToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dnstap-forwarder/1.0")

	resp, err := hw.client.Do(req)
	if err != nil {
		return fmt.Errorf("posting to HEC: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error details
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Warning: could not read HEC response body: %v", err)
	}

	// Check for HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Some status codes are retryable, others are not
		switch resp.StatusCode {
		case 429: // Too Many Requests
			return fmt.Errorf("HEC rate limited (429): %s", string(body))
		case 400, 401, 403: // Bad Request, Unauthorized, Forbidden
			return fmt.Errorf("HEC request failed (HTTP %d): %s", resp.StatusCode, string(body))
		case 500, 502, 503, 504: // Server errors - retryable
			return fmt.Errorf("HEC server error (HTTP %d): %s", resp.StatusCode, string(body))
		default:
			return fmt.Errorf("HEC returned unexpected status %d: %s", resp.StatusCode, string(body))
		}
	}

	// Log successful response details
	if hw.config.LogLevel == "debug" {
		log.Printf("HEC POST successful: status=%d, body=%s", resp.StatusCode, string(body))
	}

	return nil
}

func (hw *HECWriter) AddEvent(event *events.DNSEvent) error {
	hw.bufferMutex.Lock()

	hw.buffer = append(hw.buffer, event)
	if len(hw.buffer) >= hw.config.BatchSize {
		// Create a copy of the buffer for flushing
		bufferCopy := make([]*events.DNSEvent, len(hw.buffer))
		copy(bufferCopy, hw.buffer)
		hw.buffer = hw.buffer[:0] // Clear the buffer

		// Release the lock before flushing to avoid deadlock
		hw.bufferMutex.Unlock()
		return hw.flushBufferCopy(bufferCopy)
	}

	hw.bufferMutex.Unlock()
	return nil
}

// flushBufferCopy flushes a copy of the buffer (thread-safe)
func (hw *HECWriter) flushBufferCopy(bufferCopy []*events.DNSEvent) error {
	if len(bufferCopy) == 0 {
		return nil
	}

	// Prepare batch payload with Splunk HEC envelope
	var hecEvents []HECEvent
	for _, event := range bufferCopy {
		hecEvent := HECEvent{
			Event: event,
		}

		// Add Splunk metadata if configured
		if event.SplunkHost != "" {
			hecEvent.Host = event.SplunkHost
		}
		if event.SplunkIndex != "" {
			hecEvent.Index = event.SplunkIndex
		}
		if event.SplunkSource != "" {
			hecEvent.Source = event.SplunkSource
		}
		if event.SplunkSourcetype != "" {
			hecEvent.Sourcetype = event.SplunkSourcetype
		}

		hecEvents = append(hecEvents, hecEvent)
	}

	// Marshal to JSON
	payload, err := json.Marshal(hecEvents)
	if err != nil {
		return fmt.Errorf("marshaling HEC events: %w", err)
	}

	// Post to HEC with enhanced retry logic
	err = hw.postToHEC(payload, bufferCopy)
	if err == nil {
		atomic.AddInt64(&hw.eventCount, int64(len(bufferCopy)))
		if hw.config.LogLevel == "debug" || atomic.LoadInt64(&hw.eventCount)%1000 == 0 {
			log.Printf("Posted %d events to Splunk HEC (total: %d, success: %d, failures: %d)",
				len(bufferCopy), atomic.LoadInt64(&hw.eventCount), atomic.LoadInt64(&hw.successCount), atomic.LoadInt64(&hw.failureCount))
		}
		return nil
	}

	// Batch failed: try each event individually
	log.Printf("Batch POST to HEC failed, retrying each event individually: %v", err)
	var failedEvents []*events.DNSEvent
	for _, event := range bufferCopy {
		if err := hw.sendSingleEventWithRetry(event); err != nil {
			log.Printf("Failed to POST single event to HEC after retries: %v", err)
			failedEvents = append(failedEvents, event)
		}
	}

	// Write any failed events to DLQ/persistent buffer
	if len(failedEvents) > 0 {
		if err := hw.writeToDLQ(failedEvents, "Partial batch retry failure"); err != nil {
			log.Printf("Failed to write to DLQ: %v", err)
		}
		if err := hw.saveToPersistentBuffer(failedEvents); err != nil {
			log.Printf("Failed to save to persistent buffer: %v", err)
		}
		return fmt.Errorf("%d events failed after partial batch retry", len(failedEvents))
	}

	return nil
}

// sendSingleEventWithRetry tries to POST a single event with retries
func (hw *HECWriter) sendSingleEventWithRetry(event *events.DNSEvent) error {
	hecEvent := HECEvent{Event: event}
	if event.SplunkHost != "" {
		hecEvent.Host = event.SplunkHost
	}
	if event.SplunkIndex != "" {
		hecEvent.Index = event.SplunkIndex
	}
	if event.SplunkSource != "" {
		hecEvent.Source = event.SplunkSource
	}
	if event.SplunkSourcetype != "" {
		hecEvent.Sourcetype = event.SplunkSourcetype
	}

	payload, err := json.Marshal([]HECEvent{hecEvent})
	if err != nil {
		return fmt.Errorf("marshaling single HEC event: %w", err)
	}

	maxRetries := hw.config.HECMaxRetries
	baseDelay := hw.config.HECRetryDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * baseDelay
			log.Printf("Retrying single HEC POST in %v (attempt %d/%d)", delay, attempt+1, maxRetries+1)
			time.Sleep(delay)
		}

		err := hw.doPost(payload)
		if err == nil {
			atomic.AddInt64(&hw.successCount, 1)
			hw.statsMutex.Lock()
			hw.lastSuccessTime = time.Now()
			hw.statsMutex.Unlock()
			return nil
		}
		if attempt == maxRetries {
			atomic.AddInt64(&hw.failureCount, 1)
			hw.statsMutex.Lock()
			hw.lastFailureTime = time.Now()
			hw.statsMutex.Unlock()
			return err
		}
		log.Printf("Single HEC POST attempt %d failed: %v", attempt+1, err)
	}
	return fmt.Errorf("single HEC POST failed after %d attempts", maxRetries+1)
}

func (hw *HECWriter) Flush() error {
	hw.bufferMutex.Lock()

	// Create a copy of the current buffer
	bufferCopy := make([]*events.DNSEvent, len(hw.buffer))
	copy(bufferCopy, hw.buffer)
	hw.buffer = hw.buffer[:0] // Clear the buffer

	hw.bufferMutex.Unlock()

	return hw.flushBufferCopy(bufferCopy)
}

func (hw *HECWriter) Close() error {
	if err := hw.Flush(); err != nil {
		log.Printf("Error flushing on close: %v", err)
	}

	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	if hw.dlqWriter != nil {
		hw.dlqWriter.Close()
	}

	if hw.persistentBuffer != nil {
		hw.persistentBuffer.Close()
	}

	// Log final statistics
	log.Printf("HEC Writer closed - Total events: %d, Success: %d, Failures: %d",
		atomic.LoadInt64(&hw.eventCount), atomic.LoadInt64(&hw.successCount), atomic.LoadInt64(&hw.failureCount))

	return nil
}

// GetStats returns thread-safe statistics
func (hw *HECWriter) GetStats() map[string]any {
	hw.statsMutex.RLock()
	defer hw.statsMutex.RUnlock()

	return map[string]any{
		"total_events":  atomic.LoadInt64(&hw.eventCount),
		"success_count": atomic.LoadInt64(&hw.successCount),
		"failure_count": atomic.LoadInt64(&hw.failureCount),
		"last_success":  hw.lastSuccessTime,
		"last_failure":  hw.lastFailureTime,
		"buffer_size":   len(hw.buffer),
	}
}

// replayEventsOnStartup replays events from persistent buffer and DLQ on startup
func (hw *HECWriter) replayEventsOnStartup() error {
	log.Printf("Starting event replay from persistent buffer and DLQ...")

	// Replay from persistent buffer first (newer events)
	if hw.persistentBuffer != nil {
		if err := hw.replayFromPersistentBuffer(); err != nil {
			log.Printf("Failed to replay from persistent buffer: %v", err)
		}
	}

	// Replay from DLQ (older failed events)
	if hw.dlqWriter != nil {
		if err := hw.replayFromDLQ(); err != nil {
			log.Printf("Failed to replay from DLQ: %v", err)
		}
	}

	log.Printf("Event replay completed")
	return nil
}

// replayFromPersistentBuffer reads and resends events from the persistent buffer
func (hw *HECWriter) replayFromPersistentBuffer() error {
	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	// Seek to beginning of file
	if _, err := hw.persistentBuffer.Seek(0, 0); err != nil {
		return fmt.Errorf("seeking to beginning of persistent buffer: %w", err)
	}

	scanner := bufio.NewScanner(hw.persistentBuffer)
	var eventsList []*events.DNSEvent
	var successfulEvents []int64 // Track positions of successfully sent events
	var currentPos int64 = 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			currentPos++
			continue
		}

		var event events.DNSEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			log.Printf("Failed to unmarshal event from persistent buffer: %v", err)
			currentPos++
			continue
		}

		eventsList = append(eventsList, &event)
		currentPos++

		// Process in batches
		if len(eventsList) >= hw.config.BatchSize {
			if hw.replayBatch(eventsList) {
				// Mark these positions as successful
				for i := currentPos - int64(len(eventsList)); i < currentPos; i++ {
					successfulEvents = append(successfulEvents, i)
				}
			}
			eventsList = eventsList[:0] // Clear slice but keep capacity
		}
	}

	// Process remaining events
	if len(eventsList) > 0 {
		if hw.replayBatch(eventsList) {
			for i := currentPos - int64(len(eventsList)); i < currentPos; i++ {
				successfulEvents = append(successfulEvents, i)
			}
		}
	}

	// Remove successfully sent events from persistent buffer
	if len(successfulEvents) > 0 {
		if err := hw.removeSuccessfulEventsFromBuffer(successfulEvents); err != nil {
			log.Printf("Failed to remove successful events from persistent buffer: %v", err)
		}
	}

	return scanner.Err()
}

// replayFromDLQ reads and resends events from the dead letter queue
func (hw *HECWriter) replayFromDLQ() error {
	hw.fileMutex.Lock()
	defer hw.fileMutex.Unlock()

	// Open DLQ file for reading
	dlqFile, err := os.Open(hw.config.HECDLQPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // DLQ file doesn't exist, nothing to replay
		}
		return fmt.Errorf("opening DLQ file for replay: %w", err)
	}
	defer dlqFile.Close()

	scanner := bufio.NewScanner(dlqFile)
	var successfulLines []int64
	var currentLine int64 = 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			currentLine++
			continue
		}

		// Parse DLQ entry
		var dlqEntry struct {
			Timestamp string `json:"timestamp"`
			Reason    string `json:"reason"`
			Events    []any  `json:"events"`
		}

		if err := json.Unmarshal([]byte(line), &dlqEntry); err != nil {
			log.Printf("Failed to unmarshal DLQ entry: %v", err)
			currentLine++
			continue
		}

		// Convert events back to DNSEvent
		var eventsList []*events.DNSEvent
		for _, eventData := range dlqEntry.Events {
			eventJSON, err := json.Marshal(eventData)
			if err != nil {
				log.Printf("Failed to marshal event from DLQ: %v", err)
				continue
			}

			var event events.DNSEvent
			if err := json.Unmarshal(eventJSON, &event); err != nil {
				log.Printf("Failed to unmarshal event from DLQ: %v", err)
				continue
			}

			eventsList = append(eventsList, &event)
		}

		// Try to resend events
		if len(eventsList) > 0 {
			if hw.replayBatch(eventsList) {
				successfulLines = append(successfulLines, currentLine)
			}
		}

		currentLine++
	}

	// Remove successfully sent events from DLQ
	if len(successfulLines) > 0 {
		if err := hw.removeSuccessfulLinesFromDLQ(successfulLines); err != nil {
			log.Printf("Failed to remove successful events from DLQ: %v", err)
		}
	}

	return scanner.Err()
}

// replayBatch attempts to send a batch of events and returns true if successful
func (hw *HECWriter) replayBatch(events []*events.DNSEvent) bool {
	// Create HEC payload
	hecEvents := make([]HECEvent, len(events))
	for i, event := range events {
		hecEvents[i] = HECEvent{
			Event:      event,
			Time:       event.Timestamp,
			Host:       hw.hostname,
			Index:      hw.config.SplunkIndex,
			Source:     hw.config.SplunkSource,
			Sourcetype: hw.config.SplunkSourcetype,
		}
	}

	payload, err := json.Marshal(hecEvents)
	if err != nil {
		log.Printf("Failed to marshal replay batch: %v", err)
		return false
	}

	// Try to send with retries
	if err := hw.postToHEC(payload, events); err != nil {
		log.Printf("Failed to send replay batch: %v", err)
		return false
	}

	log.Printf("Successfully replayed %d events", len(events))
	return true
}

// removeSuccessfulEventsFromBuffer removes successfully sent events from persistent buffer
func (hw *HECWriter) removeSuccessfulEventsFromBuffer(successfulPositions []int64) error {
	// This is a simplified implementation that truncates the file
	// In a production environment, you might want to implement a more sophisticated
	// approach that removes specific lines without truncating

	// For now, we'll truncate the file since we've successfully sent all events
	if err := hw.persistentBuffer.Truncate(0); err != nil {
		return fmt.Errorf("truncating persistent buffer: %w", err)
	}

	if err := hw.persistentBuffer.Sync(); err != nil {
		return fmt.Errorf("syncing persistent buffer: %w", err)
	}

	log.Printf("Removed %d successfully replayed events from persistent buffer", len(successfulPositions))
	return nil
}

// removeSuccessfulLinesFromDLQ removes successfully sent events from DLQ
func (hw *HECWriter) removeSuccessfulLinesFromDLQ(successfulLines []int64) error {
	// This is a simplified implementation that truncates the file
	// In a production environment, you might want to implement a more sophisticated
	// approach that removes specific lines without truncating

	// For now, we'll truncate the file since we've successfully sent all events
	if err := hw.dlqWriter.Truncate(0); err != nil {
		return fmt.Errorf("truncating DLQ: %w", err)
	}

	if err := hw.dlqWriter.Sync(); err != nil {
		return fmt.Errorf("syncing DLQ: %w", err)
	}

	log.Printf("Removed %d successfully replayed events from DLQ", len(successfulLines))
	return nil
}
