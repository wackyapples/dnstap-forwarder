#!/bin/bash

# Test script for connection limits
set -e

echo "Testing DNSTAP forwarder connection limits..."

# Create a temporary socket for testing
TEST_SOCKET="/tmp/test-dnstap.sock"
TEST_OUTPUT="/tmp/test-output.log"

# Clean up any existing files
rm -f "$TEST_SOCKET" "$TEST_OUTPUT"

# Start the forwarder with a low connection limit
echo "Starting forwarder with max-connections=2..."
./dnstap-forwarder \
    -socket "$TEST_SOCKET" \
    -output-type file \
    -output-path "$TEST_OUTPUT" \
    -max-connections 2 \
    -log-level debug &
FORWARDER_PID=$!

# Wait for the forwarder to start
sleep 2

echo "Forwarder started with PID: $FORWARDER_PID"

# Function to create a test connection
create_test_connection() {
    local conn_id=$1
    echo "Creating test connection $conn_id..."
    
    # Create a simple DNSTAP frame (this is a minimal valid frame)
    # In a real test, you'd send actual DNSTAP data
    echo "test-data-$conn_id" | nc -U "$TEST_SOCKET" &
    local nc_pid=$!
    
    # Wait a bit for the connection to be established
    sleep 1
    
    # Check if the connection was accepted
    if kill -0 $nc_pid 2>/dev/null; then
        echo "Connection $conn_id established successfully"
        return 0
    else
        echo "Connection $conn_id failed"
        return 1
    fi
}

# Test connection limit
echo "Testing connection limit of 2..."

# Try to create 4 connections
for i in {1..4}; do
    if create_test_connection $i; then
        echo "✓ Connection $i successful"
    else
        echo "✗ Connection $i failed (expected for connections > 2)"
    fi
    sleep 1
done

# Wait a bit for processing
sleep 3

# Check the logs for connection limit messages
echo "Checking logs for connection limit enforcement..."
if grep -q "Connection limit reached" /dev/stderr 2>/dev/null || grep -q "Connection limit reached" /tmp/test-output.log 2>/dev/null; then
    echo "✓ Connection limit enforcement detected"
else
    echo "✗ No connection limit enforcement found in logs"
fi

# Clean up
echo "Cleaning up..."
kill $FORWARDER_PID 2>/dev/null || true
rm -f "$TEST_SOCKET" "$TEST_OUTPUT"

echo "Test completed!" 