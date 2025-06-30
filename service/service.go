package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	dnstap "github.com/dnstap/golang-dnstap"
	framestream "github.com/farsightsec/golang-framestream"
	"google.golang.org/protobuf/proto"

	"dnstap-forwarder/config"
	"dnstap-forwarder/events"
	"dnstap-forwarder/output"
)

// PerformanceMetrics tracks service performance metrics
type PerformanceMetrics struct {
	// Event processing metrics
	EventsProcessed  int64         `json:"events_processed"`
	EventsPerSecond  float64       `json:"events_per_second"`
	AverageLatency   time.Duration `json:"average_latency"`
	ProcessingErrors int64         `json:"processing_errors"`

	// Connection metrics
	ActiveConnections int64 `json:"active_connections"`
	TotalConnections  int64 `json:"total_connections"`
	ConnectionErrors  int64 `json:"connection_errors"`

	// Output metrics
	OutputSuccess int64         `json:"output_success"`
	OutputErrors  int64         `json:"output_errors"`
	OutputLatency time.Duration `json:"output_latency"`

	// System metrics
	Uptime      time.Duration `json:"uptime"`
	MemoryUsage int64         `json:"memory_usage_bytes"`
	StartTime   time.Time     `json:"start_time"`

	// Protected by mutex
	mu                sync.RWMutex
	latencySamples    []time.Duration
	maxLatencySamples int
}

// ConnectionInfo tracks connection metadata
type ConnectionInfo struct {
	ID           string
	RemoteAddr   string
	StartTime    time.Time
	MessageCount int64
	LastActivity time.Time
	ctx          context.Context
	cancel       context.CancelFunc
}

// Service represents the main service
type Service struct {
	config       config.Config
	processor    *events.Processor
	outputWriter output.Writer

	// Connection management
	activeConnections sync.Map
	connectionCount   int64
	maxConnections    int64
	shutdownChan      chan struct{}
	wg                sync.WaitGroup

	// Performance monitoring
	metrics           *PerformanceMetrics
	healthServer      *http.Server
	monitoringEnabled bool
}

// NewService creates a new service instance
func NewService(config config.Config) (*Service, error) {
	outputWriter, err := output.NewWriter(config)
	if err != nil {
		return nil, fmt.Errorf("creating output writer: %w", err)
	}

	// Get hostname for Splunk metadata if not provided
	hostname := config.SplunkHost
	if hostname == "" {
		if h, err := os.Hostname(); err == nil {
			hostname = h
		}
	}

	processor := events.NewProcessorWithSplunkMetadata(
		config.SplunkSourcetype,
		hostname,
		config.SplunkIndex,
		config.SplunkSource,
	)

	// Set reasonable connection limits based on configuration
	maxConnections := int64(100) // Default limit
	if config.MaxConnections > 0 {
		maxConnections = int64(config.MaxConnections)
	} else if config.BufferSize > 0 {
		// Scale connection limit based on buffer size if not explicitly set
		maxConnections = int64(config.BufferSize / 10)
		if maxConnections < 10 {
			maxConnections = 10
		}
		if maxConnections > 500 {
			maxConnections = 500
		}
	}

	return &Service{
		config:         config,
		processor:      processor,
		outputWriter:   outputWriter,
		maxConnections: maxConnections,
		shutdownChan:   make(chan struct{}),
		metrics: &PerformanceMetrics{
			StartTime:         time.Now(),
			maxLatencySamples: config.MaxLatencySamples,
		},
		monitoringEnabled: config.EnableMonitoring,
	}, nil
}

// Run starts the service
func (s *Service) Run() error {
	// Remove existing socket file if it exists
	if _, err := os.Stat(s.config.SocketPath); err == nil {
		if err := os.Remove(s.config.SocketPath); err != nil {
			return fmt.Errorf("removing existing socket: %w", err)
		}
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", s.config.SocketPath)
	if err != nil {
		return fmt.Errorf("creating unix socket listener: %w", err)
	}
	defer listener.Close()

	log.Printf("Listening on Unix socket: %s", s.config.SocketPath)
	log.Printf("Maximum concurrent connections: %d", s.maxConnections)

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Set up flush timer
	flushTicker := time.NewTicker(s.config.FlushInterval)
	defer flushTicker.Stop()

	// Set up connection cleanup timer
	cleanupTicker := time.NewTicker(30 * time.Second)
	defer cleanupTicker.Stop()

	// Handle shutdown
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, starting graceful shutdown...")
		close(s.shutdownChan)

		// Stop accepting new connections
		listener.Close()

		// Wait for active connections to finish (with timeout)
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Println("All connections closed gracefully")
		case <-time.After(30 * time.Second):
			log.Println("Timeout waiting for connections to close")
		}

		// Close output writer
		if err := s.outputWriter.Close(); err != nil {
			log.Printf("Error closing output writer: %v", err)
		}

		log.Println("Shutdown complete")
		os.Exit(0)
	}()

	// Handle periodic flush
	go func() {
		for {
			select {
			case <-flushTicker.C:
				if err := s.outputWriter.Flush(); err != nil {
					log.Printf("Error flushing to output: %v", err)
				}
			case <-s.shutdownChan:
				return
			}
		}
	}()

	// Handle connection cleanup
	go func() {
		for {
			select {
			case <-cleanupTicker.C:
				s.cleanupInactiveConnections()
			case <-s.shutdownChan:
				return
			}
		}
	}()

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if the error is due to the listener being closed during shutdown
			if strings.HasSuffix(err.Error(), "use of closed network connection") {
				log.Printf("Info: listener closed, shutting down")
				break
			}

			log.Printf("Error accepting connection: %v", err)
			atomic.AddInt64(&s.metrics.ConnectionErrors, 1)
			continue
		}

		// Check connection limit
		currentCount := atomic.LoadInt64(&s.connectionCount)
		if currentCount >= s.maxConnections {
			log.Printf("Connection limit reached (%d), rejecting connection from %s", s.maxConnections, conn.RemoteAddr())
			conn.Close()
			atomic.AddInt64(&s.metrics.ConnectionErrors, 1)
			continue
		}

		// Start health monitoring server if enabled
		if s.monitoringEnabled && s.healthServer == nil {
			go s.startHealthServer()
		}

		// Handle connection
		s.wg.Add(1)
		go s.handleConnection(conn)
	}

	return nil
}

// handleConnection processes a single connection
func (s *Service) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		s.wg.Done()
		atomic.AddInt64(&s.connectionCount, -1)
	}()

	// Increment connection count
	atomic.AddInt64(&s.connectionCount, 1)

	remoteAddr := conn.RemoteAddr().String()
	if remoteAddr == "" {
		remoteAddr = "unknown"
	}

	// Create connection context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create connection info
	connInfo := &ConnectionInfo{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		RemoteAddr:   remoteAddr,
		StartTime:    time.Now(),
		LastActivity: time.Now(),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Track connection
	s.activeConnections.Store(connInfo.ID, connInfo)
	defer s.activeConnections.Delete(connInfo.ID)

	log.Printf("New connection from %s (ID: %s, active: %d)",
		remoteAddr, connInfo.ID, atomic.LoadInt64(&s.connectionCount))

	// Set connection timeout
	if s.config.ConnectionTimeout > 0 {
		conn.SetDeadline(time.Now().Add(s.config.ConnectionTimeout))
	} else {
		conn.SetDeadline(time.Now().Add(5 * time.Minute))
	}

	// Create frame stream decoder
	decoder, err := framestream.NewDecoder(conn, &framestream.DecoderOptions{
		ContentType:   []byte("protobuf:dnstap.Dnstap"),
		Bidirectional: false,
	})
	if err != nil {
		log.Printf("Error creating frame stream decoder for %s: %v", remoteAddr, err)
		return
	}

	// Process messages
	messageCount := int64(0)
	for {
		select {
		case <-s.shutdownChan:
			log.Printf("Shutdown signal received, closing connection from %s", remoteAddr)
			return
		case <-ctx.Done():
			log.Printf("Connection context cancelled for %s", remoteAddr)
			return
		default:
			// Continue processing
		}

		// Set read deadline for each message
		if s.config.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		} else {
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}

		frameData, err := decoder.Decode()
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading frame from %s: %v", remoteAddr, err)
			}
			break
		}

		// Update last activity
		connInfo.LastActivity = time.Now()
		atomic.AddInt64(&connInfo.MessageCount, 1)
		messageCount++

		// Decode protobuf message
		dt := &dnstap.Dnstap{}
		if err := proto.Unmarshal(frameData, dt); err != nil {
			log.Printf("Error unmarshaling protobuf from %s: %v", remoteAddr, err)
			continue
		}

		// Process the message
		startTime := time.Now()
		event, err := s.processor.ProcessMessage(dt)
		processingLatency := time.Since(startTime)

		if err != nil {
			log.Printf("Error processing message from %s: %v", remoteAddr, err)
			s.recordProcessingError()
			continue
		}

		if event != nil {
			outputStartTime := time.Now()
			if err := s.outputWriter.AddEvent(event); err != nil {
				log.Printf("Error adding event to output from %s: %v", remoteAddr, err)
				s.recordOutputError()
			} else {
				s.recordOutputSuccess()
			}
			outputLatency := time.Since(outputStartTime)

			// Record total processing latency
			s.recordLatency(processingLatency + outputLatency)
			s.recordEventProcessed()

			// Performance logging if enabled
			if s.config.PerformanceLogging && messageCount%100 == 0 {
				log.Printf("Performance: processed %d messages, avg latency: %v, events/sec: %.2f",
					messageCount, s.metrics.AverageLatency, s.metrics.EventsPerSecond)
			}
		}

		// Log progress
		if s.config.LogLevel == "debug" && messageCount%100 == 0 {
			log.Printf("Processed %d messages from %s (ID: %s)",
				messageCount, remoteAddr, connInfo.ID)
		}
	}

	log.Printf("Connection from %s (ID: %s) closed after processing %d messages",
		remoteAddr, connInfo.ID, messageCount)
}

// cleanupInactiveConnections removes connections that have been inactive for too long
func (s *Service) cleanupInactiveConnections() {
	inactiveThreshold := 10 * time.Minute
	now := time.Now()

	s.activeConnections.Range(func(key, value any) bool {
		connInfo := value.(*ConnectionInfo)
		if now.Sub(connInfo.LastActivity) > inactiveThreshold {
			log.Printf("Closing inactive connection from %s (ID: %s, inactive for %v)",
				connInfo.RemoteAddr, connInfo.ID, now.Sub(connInfo.LastActivity))
			connInfo.cancel()
			s.activeConnections.Delete(key)
		}
		return true
	})
}

// GetConnectionStats returns current connection statistics
func (s *Service) GetConnectionStats() map[string]any {
	activeCount := int64(0)
	totalMessages := int64(0)

	s.activeConnections.Range(func(key, value any) bool {
		connInfo := value.(*ConnectionInfo)
		activeCount++
		totalMessages += atomic.LoadInt64(&connInfo.MessageCount)
		return true
	})

	return map[string]any{
		"active_connections": activeCount,
		"max_connections":    s.maxConnections,
		"total_messages":     totalMessages,
	}
}

// startHealthServer starts the HTTP health monitoring server
func (s *Service) startHealthServer() {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealth)

	// Metrics endpoint
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Stats endpoint
	mux.HandleFunc("/stats", s.handleStats)

	// Root endpoint with basic info
	mux.HandleFunc("/", s.handleRoot)

	s.healthServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.MonitoringPort),
		Handler: mux,
	}

	log.Printf("Starting health monitoring server on :%d", s.config.MonitoringPort)
	if err := s.healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("Health server error: %v", err)
	}
}

// handleHealth handles health check requests
func (s *Service) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.metrics.mu.RLock()
	uptime := time.Since(s.metrics.StartTime)
	s.metrics.mu.RUnlock()

	status := "healthy"
	if uptime < 5*time.Second {
		status = "starting"
	}

	response := map[string]interface{}{
		"status":  status,
		"uptime":  uptime.String(),
		"version": "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleMetrics handles metrics requests
func (s *Service) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.updateMetrics()

	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.metrics)
}

// handleStats handles detailed stats requests
func (s *Service) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.GetConnectionStats()

	// Add performance metrics
	s.updateMetrics()
	s.metrics.mu.RLock()
	stats["performance"] = s.metrics
	s.metrics.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleRoot handles root endpoint requests
func (s *Service) handleRoot(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"service": "dnstap-forwarder",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"health":  "/health",
			"metrics": "/metrics",
			"stats":   "/stats",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// updateMetrics updates performance metrics
func (s *Service) updateMetrics() {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()

	// Update uptime
	s.metrics.Uptime = time.Since(s.metrics.StartTime)

	// Update connection count
	s.metrics.ActiveConnections = atomic.LoadInt64(&s.connectionCount)

	// Calculate events per second
	if s.metrics.Uptime > 0 {
		s.metrics.EventsPerSecond = float64(s.metrics.EventsProcessed) / s.metrics.Uptime.Seconds()
	}

	// Calculate average latency
	if len(s.metrics.latencySamples) > 0 {
		var total time.Duration
		for _, latency := range s.metrics.latencySamples {
			total += latency
		}
		s.metrics.AverageLatency = total / time.Duration(len(s.metrics.latencySamples))
	}
}

// recordLatency records a processing latency sample
func (s *Service) recordLatency(latency time.Duration) {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()

	s.metrics.latencySamples = append(s.metrics.latencySamples, latency)

	// Keep only the last maxLatencySamples
	if len(s.metrics.latencySamples) > s.metrics.maxLatencySamples {
		s.metrics.latencySamples = s.metrics.latencySamples[len(s.metrics.latencySamples)-s.metrics.maxLatencySamples:]
	}
}

// recordEventProcessed records a successfully processed event
func (s *Service) recordEventProcessed() {
	atomic.AddInt64(&s.metrics.EventsProcessed, 1)
}

// recordProcessingError records a processing error
func (s *Service) recordProcessingError() {
	atomic.AddInt64(&s.metrics.ProcessingErrors, 1)
}

// recordOutputSuccess records a successful output operation
func (s *Service) recordOutputSuccess() {
	atomic.AddInt64(&s.metrics.OutputSuccess, 1)
}

// recordOutputError records an output error
func (s *Service) recordOutputError() {
	atomic.AddInt64(&s.metrics.OutputErrors, 1)
}
