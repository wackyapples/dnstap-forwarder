# Mock HEC Server

A simple Go-based mock server that simulates Splunk HEC (HTTP Event Collector) for testing the DNSTAP Forwarder's HEC integration.

## Features

- **Configurable Response Codes**: Simulate success (200) or various failure scenarios (400, 401, 500, etc.)
- **Response Delays**: Simulate slow network conditions or server processing delays
- **Token Validation**: Validate HEC tokens like a real Splunk server
- **Request Logging**: Log all incoming requests for debugging
- **Health and Stats Endpoints**: Monitor server status and request statistics
- **JSON Validation**: Validate incoming JSON payloads
- **Thread-Safe**: Handles concurrent requests safely

## Building

```bash
go build -o mock_hec_server main.go
```

## Usage

### Basic Usage

```bash
# Start with default settings (port 8088, token "test-token")
./mock_hec_server

# Start with custom port and tokens
./mock_hec_server -port 9000 -tokens "token1,token2,token3"

# Start with verbose logging
./mock_hec_server -verbose
```

### Command Line Options

- `-port int` - Port to listen on (default: 8088)
- `-tokens string` - Comma-separated list of valid tokens (default: "test-token,valid-token")
- `-response-code int` - HTTP response code to return (default: 200)
- `-response-delay duration` - Delay before sending response (default: 0)
- `-verbose` - Enable verbose logging

### Testing Scenarios

#### 1. Normal Operation
```bash
./mock_hec_server -port 8088 -tokens "my-token"
```

#### 2. Simulate Server Errors
```bash
./mock_hec_server -port 8088 -tokens "my-token" -response-code 500
```

#### 3. Simulate Authentication Failures
```bash
./mock_hec_server -port 8088 -tokens "my-token" -response-code 401
```

#### 4. Simulate Slow Responses
```bash
./mock_hec_server -port 8088 -tokens "my-token" -response-delay 5s
```

#### 5. Simulate Network Issues
```bash
./mock_hec_server -port 8088 -tokens "my-token" -response-code 503
```

## Endpoints

### HEC Endpoint
- **URL**: `/services/collector`
- **Method**: POST
- **Headers**: 
  - `Authorization: Splunk <token>`
  - `Content-Type: application/json`
- **Body**: JSON array of events

### Health Check
- **URL**: `/health`
- **Method**: GET
- **Response**: JSON with server status and statistics

### Statistics
- **URL**: `/stats`
- **Method**: GET
- **Response**: JSON with detailed request statistics

## Testing with DNSTAP Forwarder

### 1. Start the Mock Server
```bash
./mock_hec_server -port 8088 -tokens "test-token" -verbose
```

### 2. Start DNSTAP Forwarder
```bash
./dnstap-forwarder \
  -output-type hec \
  -hec-url http://localhost:8088/services/collector \
  -hec-token test-token \
  -batch-size 10 \
  -splunk-sourcetype dns_events \
  -splunk-host test-host \
  -log-level debug
```

### 3. Monitor Results
```bash
# Check server health
curl http://localhost:8088/health

# Check request statistics
curl http://localhost:8088/stats
```

## Example Test Script

Use the provided `test_mock_server.sh` script to run comprehensive tests:

```bash
chmod +x test_mock_server.sh
./test_mock_server.sh
```

This script demonstrates:
- Basic functionality testing
- Failure scenario simulation
- Slow response testing
- Integration with DNSTAP Forwarder

## Integration Testing

The mock server is designed to be easily integrated into Go tests:

```go
func TestHECIntegration(t *testing.T) {
    // Start mock server
    server := httptest.NewServer(http.HandlerFunc(mockHECHandler))
    defer server.Close()
    
    // Test your HEC client against the mock server
    // ...
}
```

## Troubleshooting

### Common Issues

1. **Port Already in Use**
   ```bash
   # Use a different port
   ./mock_hec_server -port 8089
   ```

2. **Token Validation Failing**
   ```bash
   # Check valid tokens
   curl http://localhost:8088/stats | jq .valid_tokens
   ```

3. **No Requests Received**
   ```bash
   # Check server is running
   curl http://localhost:8088/health
   
   # Check logs for errors
   ./mock_hec_server -verbose
   ```

### Debug Mode

Enable verbose logging to see detailed request information:

```bash
./mock_hec_server -verbose
```

This will show:
- All incoming requests
- Request headers
- Request body (truncated)
- Response details
- Error conditions

## Performance Testing

The mock server can be used for performance testing:

```bash
# Simulate slow responses to test timeout handling
./mock_hec_server -response-delay 10s

# Simulate failures to test retry logic
./mock_hec_server -response-code 500

# Monitor performance with stats endpoint
watch -n 1 'curl -s http://localhost:8088/stats | jq .'
``` 