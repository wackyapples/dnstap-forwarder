# DNSTAP Forwarder

A Go application that forwards DNSTAP messages from a Unix socket to either a named pipe, Unix domain socket, rotating files, or directly to Splunk HEC.

## Features

- Receives DNSTAP messages via Unix socket
- Processes and parses DNS query/response data
- Buffers events for efficient writing
- Outputs to named pipe, Unix socket, rotating files, or Splunk HEC
- Supports both syslog and pure JSON output formats
- Configurable batch processing for high-throughput environments
- Splunk metadata support (sourcetype, host, index, source)
- Direct Splunk HEC integration with retry logic and error handling
- Automatic reconnection on output failures
- Configurable buffer size and flush intervals
- Support for multiple output types (pipe/socket/file/hec)
- Automatic file rotation with size-based triggers and cleanup

## Usage

### Command Line Options

```bash
./dnstap-forwarder [options]
```

Options:
- `-socket string` - Unix socket path for DNSTAP input (env: DNSTAP_SOCKET_PATH)
- `-output-type string` - Output type: pipe, socket, file, or hec (env: DNSTAP_OUTPUT_TYPE)
- `-output-path string` - Output path for pipe, socket, or file (env: DNSTAP_OUTPUT_PATH)
- `-output-format string` - Output format: syslog or json (env: DNSTAP_OUTPUT_FORMAT)
- `-batch-size int` - Number of events per batch (env: DNSTAP_BATCH_SIZE)
- `-buffer int` - Buffer size for events (env: DNSTAP_BUFFER_SIZE)
- `-flush-interval int` - Flush interval in seconds (env: DNSTAP_FLUSH_INTERVAL_SEC)
- `-log-level string` - Log level: debug, info, warn, error (env: DNSTAP_LOG_LEVEL)
- `-max-file-size int` - Maximum file size in MB before rotation (env: DNSTAP_MAX_FILE_SIZE_MB)
- `-max-files int` - Maximum number of files to keep (env: DNSTAP_MAX_FILES)
- `-file-pattern string` - File pattern for rotation (env: DNSTAP_FILE_PATTERN)
- `-splunk-sourcetype string` - Splunk sourcetype for events (env: DNSTAP_SPLUNK_SOURCETYPE)
- `-splunk-host string` - Splunk host field (env: DNSTAP_SPLUNK_HOST)
- `-splunk-index string` - Splunk index for events (env: DNSTAP_SPLUNK_INDEX)
- `-splunk-source string` - Splunk source field (env: DNSTAP_SPLUNK_SOURCE)
- `-hec-url string` - Splunk HEC endpoint URL (env: DNSTAP_HEC_URL)
- `-hec-token string` - Splunk HEC token (env: DNSTAP_HEC_TOKEN)

### Environment Variables

- `DNSTAP_SOCKET_PATH` - Unix socket path (default: `/var/run/dnstap.sock`)
- `DNSTAP_OUTPUT_TYPE` - Output type: pipe, socket, file, or hec (default: `pipe`)
- `DNSTAP_OUTPUT_PATH` - Output path for pipe, socket, or file (default: `/var/log/dnstap/dnstap.log`)
- `DNSTAP_OUTPUT_FORMAT` - Output format: syslog or json (default: `syslog`)
- `DNSTAP_BATCH_SIZE` - Number of events per batch (default: `1`)
- `DNSTAP_BUFFER_SIZE` - Buffer size for events (default: `100`)
- `DNSTAP_FLUSH_INTERVAL_SEC` - Flush interval in seconds (default: `5`)
- `DNSTAP_LOG_LEVEL` - Log level (default: `info`)
- `DNSTAP_MAX_FILE_SIZE_MB` - Maximum file size in MB before rotation (default: `100`)
- `DNSTAP_MAX_FILES` - Maximum number of files to keep (default: `5`)
- `DNSTAP_FILE_PATTERN` - File pattern for rotation (default: `dnstap-%Y%m%d-%H%M%S.log`)
- `DNSTAP_SPLUNK_SOURCETYPE` - Splunk sourcetype for events (default: `dnstap`)
- `DNSTAP_SPLUNK_HOST` - Splunk host field (default: system hostname)
- `DNSTAP_SPLUNK_INDEX` - Splunk index for events
- `DNSTAP_SPLUNK_SOURCE` - Splunk source field
- `DNSTAP_HEC_URL` - Splunk HEC endpoint URL (required for hec output)
- `DNSTAP_HEC_TOKEN` - Splunk HEC token (required for hec output)

Command-line flags take precedence over environment variables.

### Output Formats

#### Syslog Format (Default)
- Traditional syslog format with JSON payload embedded
- Compatible with existing syslog infrastructure
- Includes facility, severity, timestamp, hostname, and process information

#### JSON Format
- Pure JSON output, one event per line (or JSON array for batches)
- Optimized for direct ingestion into log aggregation systems
- Easier parsing for Splunk and other SIEM platforms
- No syslog wrapper overhead

### Output Types

#### Named Pipe (FIFO)
- Creates a named pipe at the specified path
- Waits for a reader (like syslog-ng) to connect
- Suitable for local syslog integration

#### Unix Domain Socket
- Creates a Unix domain socket at the specified path
- Accepts connections from syslog clients
- More flexible for network-based logging setups

#### Rotating Files
- Creates files at the specified path with automatic rotation based on size
- Suitable for long-term logging and storage
- Automatic cleanup of old files based on maximum file count
- Files are named with timestamps for easy identification
- Supports configurable file size limits and retention policies

#### Splunk HEC (HTTP Event Collector)
- Direct integration with Splunk HEC endpoint
- Supports batch processing for high throughput
- Automatic retry logic with exponential backoff
- Proper error handling and logging
- Splunk metadata envelope support
- Configurable batch sizes and timeouts

### Batch Processing

The application supports configurable batch processing to optimize performance:

- **Batch Size**: Number of events to group before sending (default: 1)
- **JSON Arrays**: When using JSON format with batch size > 1, events are sent as JSON arrays
- **HEC Optimization**: Batches are sent as single HTTP requests to Splunk HEC

## Building

```bash
go build -o dnstap-forwarder .
```

## Integration

This forwarder is designed to work with syslog systems like syslog-ng or rsyslog that can read from named pipes or Unix sockets. The JSON payloads can be parsed and forwarded to log aggregation systems or SIEM platforms.

### Example syslog-ng configuration for pipe:
```
source s_dnstap {
    pipe("/var/run/dnstap.pipe");
};

destination d_dnstap {
    file("/var/log/dnstap.log");
};

log {
    source(s_dnstap);
    destination(d_dnstap);
};
```

### Example syslog-ng configuration for socket:
```
source s_dnstap {
    unix-stream("/var/run/dnstap.sock");
};

destination d_dnstap {
    file("/var/log/dnstap.log");
};

log {
    source(s_dnstap);
    destination(d_dnstap);
};
```

### Example file output usage:
```bash
# Basic file output with default settings
./dnstap-forwarder -output-type file -output-path /var/log/dnstap/dnstap.log

# File output with custom rotation settings
./dnstap-forwarder \
  -output-type file \
  -output-path /var/log/dnstap/dnstap.log \
  -max-file-size 50 \
  -max-files 10

# Using environment variables
export DNSTAP_OUTPUT_TYPE=file
export DNSTAP_OUTPUT_PATH=/var/log/dnstap/dnstap.log
export DNSTAP_MAX_FILE_SIZE_MB=100
export DNSTAP_MAX_FILES=5
./dnstap-forwarder 
```

### JSON Format Usage:
```bash
# Output pure JSON format to file
./dnstap-forwarder \
  -output-type file \
  -output-path /var/log/dnstap/dnstap.json \
  -output-format json

# Output JSON format to pipe for Splunk ingestion
./dnstap-forwarder \
  -output-type pipe \
  -output-path /var/run/dnstap.pipe \
  -output-format json

# Using environment variables for JSON output
export DNSTAP_OUTPUT_FORMAT=json
export DNSTAP_OUTPUT_TYPE=file
export DNSTAP_OUTPUT_PATH=/var/log/dnstap/dnstap.json
./dnstap-forwarder
```

### Batch Processing Usage:
```bash
# Batch processing with JSON output
./dnstap-forwarder \
  -output-type file \
  -output-path /var/log/dnstap/dnstap.json \
  -output-format json \
  -batch-size 100

# Batch processing with syslog output
./dnstap-forwarder \
  -output-type pipe \
  -output-path /var/run/dnstap.pipe \
  -batch-size 50
```

### Splunk HEC Usage:
```bash
# Direct Splunk HEC integration
./dnstap-forwarder \
  -output-type hec \
  -hec-url https://splunk.example.com:8088/services/collector \
  -hec-token your-hec-token \
  -batch-size 100 \
  -splunk-sourcetype dns_events \
  -splunk-index dns_logs \
  -splunk-source dnstap_forwarder

# Using environment variables for HEC
export DNSTAP_OUTPUT_TYPE=hec
export DNSTAP_HEC_URL=https://splunk.example.com:8088/services/collector
export DNSTAP_HEC_TOKEN=your-hec-token
export DNSTAP_BATCH_SIZE=100
export DNSTAP_SPLUNK_SOURCETYPE=dns_events
export DNSTAP_SPLUNK_INDEX=dns_logs
export DNSTAP_SPLUNK_SOURCE=dnstap_forwarder
./dnstap-forwarder
```

### Splunk Configuration

To use the HEC output type, you need to:

1. **Enable HEC in Splunk:**
   - Go to Settings → Data Inputs → HTTP Event Collector
   - Create a new token
   - Configure the token settings (index, sourcetype, etc.)

2. **Configure the forwarder:**
   - Set `-output-type hec`
   - Provide the HEC URL and token
   - Configure batch size for optimal performance
   - Set Splunk metadata fields as needed

3. **Monitor and troubleshoot:**
   - Check application logs for HEC connection status
   - Monitor Splunk for incoming events
   - Use debug logging for detailed HEC communication