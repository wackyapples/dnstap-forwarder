package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"dnstap-forwarder/config"
	"dnstap-forwarder/service"
)

func TestPerformanceMonitoring(t *testing.T) {
	fmt.Println("Testing performance monitoring features...")

	// Create configuration with monitoring enabled
	cfg := config.Config{
		OutputType:         "file",
		OutputPath:         "/tmp/test-monitoring.log",
		BufferSize:         100,
		BatchSize:          10,
		FlushInterval:      1 * time.Second,
		LogLevel:           "info",
		EnableMonitoring:   true,
		MonitoringPort:     8081, // Use different port for testing
		ConnectionTimeout:  60 * time.Second,
		ReadTimeout:        10 * time.Second,
		MaxLatencySamples:  500,
		PerformanceLogging: true,
	}

	// Create service
	svc, err := service.NewService(cfg)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start service in background
	go func() {
		if err := svc.Run(); err != nil {
			log.Printf("Service error: %v", err)
		}
	}()

	// Wait for service to start
	time.Sleep(2 * time.Second)

	// Test health endpoint
	fmt.Println("Testing health endpoint...")
	resp, err := http.Get("http://localhost:8081/health")
	if err != nil {
		t.Logf("Health check failed: %v", err)
	} else {
		defer resp.Body.Close()
		var health map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
			t.Logf("Failed to decode health response: %v", err)
		} else {
			fmt.Printf("Health status: %s, uptime: %s\n", health["status"], health["uptime"])
		}
	}

	// Test metrics endpoint
	fmt.Println("Testing metrics endpoint...")
	resp, err = http.Get("http://localhost:8081/metrics")
	if err != nil {
		t.Logf("Metrics check failed: %v", err)
	} else {
		defer resp.Body.Close()
		var metrics map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
			t.Logf("Failed to decode metrics response: %v", err)
		} else {
			fmt.Printf("Events processed: %v, events/sec: %v\n",
				metrics["events_processed"], metrics["events_per_second"])
		}
	}

	// Test stats endpoint
	fmt.Println("Testing stats endpoint...")
	resp, err = http.Get("http://localhost:8081/stats")
	if err != nil {
		t.Logf("Stats check failed: %v", err)
	} else {
		defer resp.Body.Close()
		var stats map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Logf("Failed to decode stats response: %v", err)
		} else {
			fmt.Printf("Active connections: %v, max connections: %v\n",
				stats["active_connections"], stats["max_connections"])
		}
	}

	// Test root endpoint
	fmt.Println("Testing root endpoint...")
	resp, err = http.Get("http://localhost:8081/")
	if err != nil {
		t.Logf("Root check failed: %v", err)
	} else {
		defer resp.Body.Close()
		var info map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			t.Logf("Failed to decode root response: %v", err)
		} else {
			fmt.Printf("Service: %s, version: %s\n", info["service"], info["version"])
		}
	}

	fmt.Println("Performance monitoring test completed!")
}
