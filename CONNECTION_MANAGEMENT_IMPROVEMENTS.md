# Connection Management Improvements

## Overview
This document outlines the critical improvements made to the DNSTAP forwarder's connection handling and resource management to address high-volume, long-running environment requirements.

## Issues Addressed

### 1. **Unlimited Goroutine Creation**
**Problem**: Each incoming connection spawned a goroutine without limits, leading to potential resource exhaustion.

**Solution**: 
- Implemented configurable connection limits (`MaxConnections`)
- Added connection counting with atomic operations
- Reject connections when limit is reached

### 2. **No Connection Tracking**
**Problem**: No visibility into active connections or their health.

**Solution**:
- Added `ConnectionInfo` struct to track connection metadata
- Implemented connection tracking with `sync.Map`
- Added connection statistics and monitoring capabilities

### 3. **Poor Graceful Shutdown**
**Problem**: Abrupt shutdown could leave connections hanging and resources leaked.

**Solution**:
- Implemented graceful shutdown with context cancellation
- Added timeout-based shutdown with `sync.WaitGroup`
- Proper cleanup of all resources during shutdown

### 4. **Resource Leaks**
**Problem**: No cleanup of abandoned or inactive connections.

**Solution**:
- Added connection timeouts and deadlines
- Implemented periodic cleanup of inactive connections
- Proper resource cleanup in defer statements

## Key Features Added

### Connection Limits
```bash
# Set maximum concurrent connections
./dnstap-forwarder -max-connections 100

# Or via environment variable
export DNSTAP_MAX_CONNECTIONS=100
```

### Connection Tracking
- Each connection gets a unique ID
- Track message count per connection
- Monitor connection activity and duration
- Automatic cleanup of inactive connections (10-minute timeout)

### Graceful Shutdown
- Signal handling (SIGINT, SIGTERM)
- 30-second timeout for connection cleanup
- Proper resource deallocation
- Clean exit with status reporting

### Connection Statistics
- Active connection count
- Total messages processed
- Connection health monitoring
- Debug logging with connection IDs

## Configuration Options

| Option | Environment Variable | Default | Description |
|--------|---------------------|---------|-------------|
| `-max-connections` | `DNSTAP_MAX_CONNECTIONS` | Auto (10-500) | Maximum concurrent connections |
| `-log-level` | `DNSTAP_LOG_LEVEL` | info | Logging verbosity |

## Auto-Scaling Logic
When `MaxConnections` is not explicitly set, the system automatically scales based on buffer size:
- Minimum: 10 connections
- Maximum: 500 connections
- Formula: `BufferSize / 10` (clamped to min/max)

## Testing
A test script (`test_connection_limits.sh`) is provided to verify:
- Connection limit enforcement
- Graceful rejection of excess connections
- Proper logging of limit violations

## Performance Impact

### Memory Usage
- **Before**: Unbounded memory growth due to unlimited goroutines
- **After**: Predictable memory usage with connection limits

### Resource Management
- **Before**: Potential file descriptor exhaustion
- **After**: Controlled resource usage with proper cleanup

### Reliability
- **Before**: Crashes under high load
- **After**: Graceful degradation with connection limits

## Monitoring and Observability

### Log Messages
- Connection establishment with ID and count
- Connection limit enforcement
- Graceful shutdown progress
- Inactive connection cleanup

### Statistics
- Active connection count
- Total messages processed
- Connection health metrics

## Next Steps

This improvement addresses the most critical resource management issues. Future enhancements could include:

1. **Connection Pooling**: Reuse connections for better performance
2. **Load Balancing**: Distribute connections across multiple workers
3. **Metrics Export**: Prometheus metrics for monitoring
4. **Health Checks**: HTTP endpoint for health monitoring
5. **Circuit Breaker**: Connection-level circuit breaker patterns

## Files Modified

- `service/service.go`: Complete rewrite of connection handling
- `config/config.go`: Added `MaxConnections` configuration
- `test_connection_limits.sh`: Test script for validation

## Usage Example

```bash
# High-volume production configuration
./dnstap-forwarder \
    -max-connections 200 \
    -buffer-size 1000 \
    -batch-size 100 \
    -flush-interval 1 \
    -log-level info \
    -output-type hec \
    -hec-url "https://splunk.example.com:8088/services/collector" \
    -hec-token "your-token"
```

This configuration is suitable for processing thousands of DNS events per second while maintaining stable resource usage. 