package main

import (
	"log"

	"dnstap-forwarder/config"
	"dnstap-forwarder/service"
)

func main() {
	cfg := config.Parse()

	svc, err := service.NewService(cfg)
	if err != nil {
		log.Fatalf("Failed to create service: %v", err)
	}

	log.Printf("Starting DNSTAP to %s forwarder", cfg.OutputType)
	log.Printf("Socket path: %s", cfg.SocketPath)
	log.Printf("Output type: %s", cfg.OutputType)
	log.Printf("Output path: %s", cfg.OutputPath)
	log.Printf("Output format: %s", cfg.OutputFormat)
	log.Printf("Buffer size: %d", cfg.BufferSize)
	log.Printf("Flush interval: %s", cfg.FlushInterval)
	log.Printf("Log level: %s", cfg.LogLevel)

	// Log Splunk metadata configuration
	if cfg.SplunkSourcetype != "" {
		log.Printf("Splunk sourcetype: %s", cfg.SplunkSourcetype)
	}
	if cfg.SplunkHost != "" {
		log.Printf("Splunk host: %s", cfg.SplunkHost)
	}
	if cfg.SplunkIndex != "" {
		log.Printf("Splunk index: %s", cfg.SplunkIndex)
	}
	if cfg.SplunkSource != "" {
		log.Printf("Splunk source: %s", cfg.SplunkSource)
	}

	if err := svc.Run(); err != nil {
		log.Fatalf("Service error: %v", err)
	}
}
