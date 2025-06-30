package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MockHECServer represents a mock Splunk HEC server
type MockHECServer struct {
	port            int
	validTokens     map[string]bool
	responseCode    int
	responseDelay   time.Duration
	requestCount    int64
	successCount    int64
	failureCount    int64
	lastRequestTime time.Time
	mutex           sync.RWMutex
}

// HECResponse represents a Splunk HEC response
type HECResponse struct {
	Text               string `json:"text"`
	Code               int    `json:"code"`
	InvalidEventNumber int    `json:"invalid_event_number,omitempty"`
}

func main() {
	port := flag.Int("port", 8088, "Port to listen on")
	tokens := flag.String("tokens", "test-token,valid-token", "Comma-separated list of valid tokens")
	responseCode := flag.Int("response-code", 200, "HTTP response code to return")
	responseDelay := flag.Duration("response-delay", 0, "Delay before sending response")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Parse valid tokens
	validTokens := make(map[string]bool)
	for _, token := range strings.Split(*tokens, ",") {
		validTokens[strings.TrimSpace(token)] = true
	}

	server := &MockHECServer{
		port:          *port,
		validTokens:   validTokens,
		responseCode:  *responseCode,
		responseDelay: *responseDelay,
	}

	// Set up HTTP handlers
	http.HandleFunc("/services/collector", server.handleHEC)
	http.HandleFunc("/health", server.handleHealth)
	http.HandleFunc("/stats", server.handleStats)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting mock HEC server on port %d", *port)
	log.Printf("Valid tokens: %v", validTokens)
	log.Printf("Response code: %d", *responseCode)
	log.Printf("Response delay: %v", *responseDelay)
	log.Printf("Health check: http://localhost%s/health", addr)
	log.Printf("Stats: http://localhost%s/stats", addr)

	if *verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (s *MockHECServer) handleHEC(w http.ResponseWriter, r *http.Request) {
	s.mutex.Lock()
	s.requestCount++
	s.lastRequestTime = time.Now()
	s.mutex.Unlock()

	// Log request details
	log.Printf("Received HEC request: %s %s", r.Method, r.URL.Path)
	log.Printf("Headers: Authorization=%s, Content-Type=%s",
		r.Header.Get("Authorization"), r.Header.Get("Content-Type"))

	// Check authorization
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		s.respondWithError(w, 401, "Missing Authorization header")
		return
	}

	if !strings.HasPrefix(authHeader, "Splunk ") {
		s.respondWithError(w, 401, "Invalid Authorization format")
		return
	}

	token := strings.TrimPrefix(authHeader, "Splunk ")
	if !s.validTokens[token] {
		s.respondWithError(w, 401, "Invalid token")
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondWithError(w, 400, "Failed to read request body")
		return
	}

	// Log request body (truncated for readability)
	bodyStr := string(body)
	if len(bodyStr) > 500 {
		bodyStr = bodyStr[:500] + "... (truncated)"
	}
	log.Printf("Request body: %s", bodyStr)

	// Simulate response delay
	if s.responseDelay > 0 {
		time.Sleep(s.responseDelay)
	}

	// Parse and validate JSON
	var events []interface{}
	if err := json.Unmarshal(body, &events); err != nil {
		s.respondWithError(w, 400, "Invalid JSON")
		return
	}

	log.Printf("Received %d events", len(events))

	// Return configured response
	if s.responseCode >= 200 && s.responseCode < 300 {
		s.mutex.Lock()
		s.successCount++
		s.mutex.Unlock()

		response := HECResponse{
			Text: "Success",
			Code: 0,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.responseCode)
		json.NewEncoder(w).Encode(response)

		log.Printf("Responding with success (HTTP %d)", s.responseCode)
	} else {
		s.mutex.Lock()
		s.failureCount++
		s.mutex.Unlock()

		s.respondWithError(w, s.responseCode, "Simulated failure")
	}
}

func (s *MockHECServer) respondWithError(w http.ResponseWriter, code int, message string) {
	response := HECResponse{
		Text: message,
		Code: code,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(response)

	log.Printf("Responding with error: HTTP %d - %s", code, message)
}

func (s *MockHECServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	status := struct {
		Status          string    `json:"status"`
		Uptime          string    `json:"uptime"`
		RequestCount    int64     `json:"request_count"`
		SuccessCount    int64     `json:"success_count"`
		FailureCount    int64     `json:"failure_count"`
		LastRequestTime time.Time `json:"last_request_time"`
		ResponseCode    int       `json:"response_code"`
		ResponseDelay   string    `json:"response_delay"`
	}{
		Status:          "healthy",
		RequestCount:    s.requestCount,
		SuccessCount:    s.successCount,
		FailureCount:    s.failureCount,
		LastRequestTime: s.lastRequestTime,
		ResponseCode:    s.responseCode,
		ResponseDelay:   s.responseDelay.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *MockHECServer) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := struct {
		RequestCount    int64     `json:"request_count"`
		SuccessCount    int64     `json:"success_count"`
		FailureCount    int64     `json:"failure_count"`
		SuccessRate     float64   `json:"success_rate"`
		LastRequestTime time.Time `json:"last_request_time"`
		ValidTokens     []string  `json:"valid_tokens"`
	}{
		RequestCount:    s.requestCount,
		SuccessCount:    s.successCount,
		FailureCount:    s.failureCount,
		LastRequestTime: s.lastRequestTime,
	}

	if s.requestCount > 0 {
		stats.SuccessRate = float64(s.successCount) / float64(s.requestCount) * 100
	}

	for token := range s.validTokens {
		stats.ValidTokens = append(stats.ValidTokens, token)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
