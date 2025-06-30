package config

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// OutputType represents the type of output
type OutputType string

const (
	OutputTypePipe   OutputType = "pipe"
	OutputTypeSocket OutputType = "socket"
	OutputTypeFile   OutputType = "file"
	OutputTypeHEC    OutputType = "hec"
)

// OutputFormat represents the format of the output
type OutputFormat string

const (
	OutputFormatSyslog OutputFormat = "syslog"
	OutputFormatJSON   OutputFormat = "json"
)

// Config holds the service configuration
type Config struct {
	SocketPath     string
	OutputType     OutputType
	OutputPath     string
	OutputFormat   OutputFormat
	BufferSize     int
	BatchSize      int // Number of events per batch
	FlushInterval  time.Duration
	LogLevel       string
	MaxConnections int // Maximum number of concurrent connections
	// File-specific configuration
	MaxFileSize int64  // Maximum file size in bytes before rotation
	MaxFiles    int    // Maximum number of files to keep
	FilePattern string // File pattern for rotation (e.g., "dnstap-%Y%m%d-%H%M%S.log")
	// Splunk metadata configuration
	SplunkSourcetype string // Splunk sourcetype for the events
	SplunkHost       string // Splunk host field (defaults to system hostname)
	SplunkIndex      string // Splunk index for the events
	SplunkSource     string // Splunk source field
	// Splunk HEC configuration
	HECURL   string // Splunk HEC endpoint URL
	HECToken string // Splunk HEC token
	// HEC reliability configuration
	HECMaxRetries     int           // Maximum retry attempts for HEC requests
	HECRetryDelay     time.Duration // Base delay between retries
	HECCircuitBreaker bool          // Enable circuit breaker pattern
	HECDLQPath        string        // Dead letter queue file path
	HECPersistentBuf  bool          // Enable persistent buffering to disk
	HECBufPath        string        // Persistent buffer file path
	HECBufMaxSize     int64         // Maximum buffer file size in MB
	// Performance and monitoring configuration
	EnableMonitoring   bool          // Enable HTTP monitoring endpoints
	MonitoringPort     int           // Port for monitoring HTTP server
	ConnectionTimeout  time.Duration // Connection timeout for DNSTAP clients
	ReadTimeout        time.Duration // Read timeout for individual messages
	MaxLatencySamples  int           // Maximum number of latency samples to keep
	PerformanceLogging bool          // Enable detailed performance logging
}

// Parse parses configuration from both environment variables and command-line flags
// Command-line flags take precedence over environment variables
func Parse() Config {
	// Set up custom usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "DNSTAP to Named Pipe/Socket forwarder\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_SOCKET_PATH        Unix socket path for DNSTAP input (default: /var/run/dnstap.sock)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_OUTPUT_TYPE        Output type: pipe, socket, or file (default: pipe)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_OUTPUT_PATH        Output path for pipe, socket, or file (default: /var/log/dnstap/dnstap.log)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_OUTPUT_FORMAT      Output format: syslog or json (default: syslog)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_BUFFER_SIZE        Buffer size for events (default: 100)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_BATCH_SIZE         Number of events per batch (default: 1)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_FLUSH_INTERVAL_SEC Flush interval in seconds (default: 5)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_LOG_LEVEL          Log level: debug, info, warn, error (default: info)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_MAX_CONNECTIONS    Maximum number of concurrent connections (default: auto)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_MAX_FILE_SIZE_MB   Maximum file size in MB before rotation (default: 100)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_MAX_FILES          Maximum number of files to keep (default: 5)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_FILE_PATTERN       File pattern for rotation (default: dnstap-%%Y%%m%%d-%%H%%M%%S.log)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_SPLUNK_SOURCETYPE  Splunk sourcetype for events (default: dnstap)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_SPLUNK_HOST        Splunk host field (default: system hostname)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_SPLUNK_INDEX       Splunk index for events\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_SPLUNK_SOURCE      Splunk source field\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_URL            Splunk HEC endpoint URL (required for hec output)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_TOKEN          Splunk HEC token (required for hec output)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_MAX_RETRIES    Maximum HEC retry attempts (default: 3)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_RETRY_DELAY_SEC HEC retry delay in seconds (default: 1)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_CIRCUIT_BREAKER Enable HEC circuit breaker (default: false)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_DLQ_PATH       HEC dead letter queue path\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_PERSISTENT_BUF  Enable HEC persistent buffering (default: false)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_BUF_PATH       HEC persistent buffer path\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_HEC_BUF_MAX_SIZE_MB HEC buffer max size in MB (default: 100)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_ENABLE_MONITORING   Enable HTTP monitoring endpoints (default: false)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_MONITORING_PORT     Port for monitoring HTTP server (default: 8080)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_CONNECTION_TIMEOUT_SEC Connection timeout in seconds (default: 300)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_READ_TIMEOUT_SEC    Read timeout in seconds (default: 30)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_MAX_LATENCY_SAMPLES Maximum latency samples to keep (default: 1000)\n")
		fmt.Fprintf(os.Stderr, "  DNSTAP_PERFORMANCE_LOGGING Enable detailed performance logging (default: false)\n")
		fmt.Fprintf(os.Stderr, "\nCommand-line flags take precedence over environment variables.\n")
	}

	// Define command-line flags
	socketPath := flag.String("socket", "", "Unix socket path for DNSTAP input (env: DNSTAP_SOCKET_PATH)")
	outputType := flag.String("output-type", "", "Output type: pipe, socket, or file (env: DNSTAP_OUTPUT_TYPE)")
	outputPath := flag.String("output-path", "", "Output path for pipe, socket, or file (env: DNSTAP_OUTPUT_PATH)")
	outputFormat := flag.String("output-format", "", "Output format: syslog or json (env: DNSTAP_OUTPUT_FORMAT)")
	bufferSize := flag.Int("buffer", 0, "Buffer size for events (env: DNSTAP_BUFFER_SIZE)")
	batchSize := flag.Int("batch-size", 0, "Number of events per batch (env: DNSTAP_BATCH_SIZE)")
	flushInterval := flag.Int("flush-interval", 0, "Flush interval in seconds (env: DNSTAP_FLUSH_INTERVAL_SEC)")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error (env: DNSTAP_LOG_LEVEL)")
	maxFileSize := flag.Int64("max-file-size", 0, "Maximum file size in MB before rotation (env: DNSTAP_MAX_FILE_SIZE_MB)")
	maxFiles := flag.Int("max-files", 0, "Maximum number of files to keep (env: DNSTAP_MAX_FILES)")
	filePattern := flag.String("file-pattern", "", "File pattern for rotation (env: DNSTAP_FILE_PATTERN)")
	splunkSourcetype := flag.String("splunk-sourcetype", "", "Splunk sourcetype for events (env: DNSTAP_SPLUNK_SOURCETYPE)")
	splunkHost := flag.String("splunk-host", "", "Splunk host field (env: DNSTAP_SPLUNK_HOST)")
	splunkIndex := flag.String("splunk-index", "", "Splunk index for events (env: DNSTAP_SPLUNK_INDEX)")
	splunkSource := flag.String("splunk-source", "", "Splunk source field (env: DNSTAP_SPLUNK_SOURCE)")
	hecURL := flag.String("hec-url", "", "Splunk HEC endpoint URL (env: DNSTAP_HEC_URL)")
	hecToken := flag.String("hec-token", "", "Splunk HEC token (env: DNSTAP_HEC_TOKEN)")
	hecMaxRetries := flag.Int("hec-max-retries", 0, "Maximum HEC retry attempts (env: DNSTAP_HEC_MAX_RETRIES)")
	hecRetryDelay := flag.Int("hec-retry-delay", 0, "HEC retry delay in seconds (env: DNSTAP_HEC_RETRY_DELAY_SEC)")
	hecCircuitBreaker := flag.Bool("hec-circuit-breaker", false, "Enable HEC circuit breaker (env: DNSTAP_HEC_CIRCUIT_BREAKER)")
	hecDLQPath := flag.String("hec-dlq-path", "", "HEC dead letter queue path (env: DNSTAP_HEC_DLQ_PATH)")
	hecPersistentBuf := flag.Bool("hec-persistent-buf", false, "Enable HEC persistent buffering (env: DNSTAP_HEC_PERSISTENT_BUF)")
	hecBufPath := flag.String("hec-buf-path", "", "HEC persistent buffer path (env: DNSTAP_HEC_BUF_PATH)")
	hecBufMaxSize := flag.Int64("hec-buf-max-size", 0, "HEC buffer max size in MB (env: DNSTAP_HEC_BUF_MAX_SIZE_MB)")
	maxConnections := flag.Int("max-connections", 0, "Maximum number of concurrent connections (env: DNSTAP_MAX_CONNECTIONS)")
	// Performance and monitoring flags
	enableMonitoring := flag.Bool("enable-monitoring", false, "Enable HTTP monitoring endpoints (env: DNSTAP_ENABLE_MONITORING)")
	monitoringPort := flag.Int("monitoring-port", 0, "Port for monitoring HTTP server (env: DNSTAP_MONITORING_PORT)")
	connectionTimeout := flag.Int("connection-timeout", 0, "Connection timeout in seconds (env: DNSTAP_CONNECTION_TIMEOUT_SEC)")
	readTimeout := flag.Int("read-timeout", 0, "Read timeout in seconds (env: DNSTAP_READ_TIMEOUT_SEC)")
	maxLatencySamples := flag.Int("max-latency-samples", 0, "Maximum latency samples to keep (env: DNSTAP_MAX_LATENCY_SAMPLES)")
	performanceLogging := flag.Bool("performance-logging", false, "Enable detailed performance logging (env: DNSTAP_PERFORMANCE_LOGGING)")

	// Parse command-line flags
	flag.Parse()

	// Get values with precedence: flag > env var > default
	config := Config{
		SocketPath:         getConfigValue(*socketPath, "DNSTAP_SOCKET_PATH", "/var/run/dnstap.sock"),
		OutputType:         OutputType(getConfigValue(*outputType, "DNSTAP_OUTPUT_TYPE", "pipe")),
		OutputPath:         getConfigValue(*outputPath, "DNSTAP_OUTPUT_PATH", "/var/log/dnstap/dnstap.log"),
		OutputFormat:       OutputFormat(getConfigValue(*outputFormat, "DNSTAP_OUTPUT_FORMAT", "syslog")),
		BufferSize:         getConfigIntValue(*bufferSize, "DNSTAP_BUFFER_SIZE", 100),
		BatchSize:          getConfigIntValue(*batchSize, "DNSTAP_BATCH_SIZE", 1),
		FlushInterval:      time.Duration(getConfigIntValue(*flushInterval, "DNSTAP_FLUSH_INTERVAL_SEC", 5)) * time.Second,
		LogLevel:           getConfigValue(*logLevel, "DNSTAP_LOG_LEVEL", "info"),
		MaxFileSize:        getConfigInt64Value(*maxFileSize, "DNSTAP_MAX_FILE_SIZE_MB", 100) * 1024 * 1024, // Convert MB to bytes
		MaxFiles:           getConfigIntValue(*maxFiles, "DNSTAP_MAX_FILES", 5),
		FilePattern:        getConfigValue(*filePattern, "DNSTAP_FILE_PATTERN", "dnstap-%Y%m%d-%H%M%S.log"),
		SplunkSourcetype:   getConfigValue(*splunkSourcetype, "DNSTAP_SPLUNK_SOURCETYPE", "dnstap"),
		SplunkHost:         getConfigValue(*splunkHost, "DNSTAP_SPLUNK_HOST", ""),
		SplunkIndex:        getConfigValue(*splunkIndex, "DNSTAP_SPLUNK_INDEX", ""),
		SplunkSource:       getConfigValue(*splunkSource, "DNSTAP_SPLUNK_SOURCE", ""),
		HECURL:             getConfigValue(*hecURL, "DNSTAP_HEC_URL", ""),
		HECToken:           getConfigValue(*hecToken, "DNSTAP_HEC_TOKEN", ""),
		HECMaxRetries:      getConfigIntValue(*hecMaxRetries, "DNSTAP_HEC_MAX_RETRIES", 3),
		HECRetryDelay:      time.Duration(getConfigIntValue(*hecRetryDelay, "DNSTAP_HEC_RETRY_DELAY_SEC", 1)) * time.Second,
		HECCircuitBreaker:  getConfigBoolValue(*hecCircuitBreaker, "DNSTAP_HEC_CIRCUIT_BREAKER", false),
		HECDLQPath:         getConfigValue(*hecDLQPath, "DNSTAP_HEC_DLQ_PATH", ""),
		HECPersistentBuf:   getConfigBoolValue(*hecPersistentBuf, "DNSTAP_HEC_PERSISTENT_BUF", false),
		HECBufPath:         getConfigValue(*hecBufPath, "DNSTAP_HEC_BUF_PATH", ""),
		HECBufMaxSize:      getConfigInt64Value(*hecBufMaxSize, "DNSTAP_HEC_BUF_MAX_SIZE_MB", 100) * 1024 * 1024, // Convert MB to bytes
		MaxConnections:     getConfigIntValue(*maxConnections, "DNSTAP_MAX_CONNECTIONS", 0),
		EnableMonitoring:   getConfigBoolValue(*enableMonitoring, "DNSTAP_ENABLE_MONITORING", false),
		MonitoringPort:     getConfigIntValue(*monitoringPort, "DNSTAP_MONITORING_PORT", 8080),
		ConnectionTimeout:  time.Duration(getConfigIntValue(*connectionTimeout, "DNSTAP_CONNECTION_TIMEOUT_SEC", 300)) * time.Second,
		ReadTimeout:        time.Duration(getConfigIntValue(*readTimeout, "DNSTAP_READ_TIMEOUT_SEC", 30)) * time.Second,
		MaxLatencySamples:  getConfigIntValue(*maxLatencySamples, "DNSTAP_MAX_LATENCY_SAMPLES", 1000),
		PerformanceLogging: getConfigBoolValue(*performanceLogging, "DNSTAP_PERFORMANCE_LOGGING", false),
	}

	return config
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := fmt.Sscanf(value, "%d", &defaultValue); err == nil && intValue == 1 {
			return defaultValue
		}
	}
	return defaultValue
}

// getConfigValue returns the value with precedence: flag > env var > default
func getConfigValue(flagValue, envKey, defaultValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return getEnv(envKey, defaultValue)
}

// getConfigIntValue returns the integer value with precedence: flag > env var > default
func getConfigIntValue(flagValue int, envKey string, defaultValue int) int {
	if flagValue > 0 {
		return flagValue
	}
	return getEnvInt(envKey, defaultValue)
}

// getConfigInt64Value returns the int64 value with precedence: flag > env var > default
func getConfigInt64Value(flagValue int64, envKey string, defaultValue int64) int64 {
	if flagValue > 0 {
		return flagValue
	}
	return getEnvInt64(envKey, defaultValue)
}

// getEnvInt64 gets an int64 environment variable with a default value
func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		var intValue int64
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getConfigBoolValue returns the boolean value with precedence: flag > env var > default
func getConfigBoolValue(flagValue bool, envKey string, defaultValue bool) bool {
	if flagValue {
		return flagValue
	}
	return getEnvBool(envKey, defaultValue)
}

// getEnvBool gets a boolean environment variable with a default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := fmt.Sscanf(value, "%t", &defaultValue); err == nil && boolValue == 1 {
			return defaultValue
		}
	}
	return defaultValue
}
