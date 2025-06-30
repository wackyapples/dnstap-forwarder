#!/bin/bash

# Test script for mock HEC server
# This script demonstrates various testing scenarios

set -e

echo "=== Mock HEC Server Test Script ==="
echo

# Build the mock server
echo "Building mock HEC server..."
cd mock_hec_server
go build -o mock_hec_server main.go
cd ..

# Function to cleanup background processes
cleanup() {
    echo "Cleaning up..."
    pkill -f mock_hec_server || true
    pkill -f dnstap-forwarder || true
}

# Set up cleanup on script exit
trap cleanup EXIT

# Test 1: Basic functionality
echo "=== Test 1: Basic HEC Server ==="
echo "Starting mock server with default settings..."
./mock_hec_server/mock_hec_server -port 8088 -tokens "test-token" &
MOCK_PID=$!

# Wait for server to start
sleep 2

echo "Testing health endpoint..."
curl -s http://localhost:8088/health | jq .

echo "Testing stats endpoint..."
curl -s http://localhost:8088/stats | jq .

echo "Testing HEC endpoint with valid token..."
curl -s -X POST http://localhost:8088/services/collector \
  -H "Authorization: Splunk test-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

echo "Testing HEC endpoint with invalid token..."
curl -s -X POST http://localhost:8088/services/collector \
  -H "Authorization: Splunk invalid-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

kill $MOCK_PID
sleep 1

# Test 2: Simulate failures
echo
echo "=== Test 2: Simulate HEC Failures ==="
echo "Starting mock server that returns 500 errors..."
./mock_hec_server/mock_hec_server -port 8088 -tokens "test-token" -response-code 500 &
MOCK_PID=$!

sleep 2

echo "Testing HEC endpoint with 500 response..."
curl -s -X POST http://localhost:8088/services/collector \
  -H "Authorization: Splunk test-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

kill $MOCK_PID
sleep 1

# Test 3: Simulate slow responses
echo
echo "=== Test 3: Simulate Slow Responses ==="
echo "Starting mock server with 2 second delay..."
./mock_hec_server/mock_hec_server -port 8088 -tokens "test-token" -response-delay 2s &
MOCK_PID=$!

sleep 2

echo "Testing HEC endpoint with delay (this will take 2 seconds)..."
time curl -s -X POST http://localhost:8088/services/collector \
  -H "Authorization: Splunk test-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

kill $MOCK_PID
sleep 1

# Test 4: Test with dnstap-forwarder
echo
echo "=== Test 4: Test with dnstap-forwarder ==="
echo "Starting mock server..."
./mock_hec_server/mock_hec_server -port 8088 -tokens "test-token" -verbose &
MOCK_PID=$!

sleep 2

echo "Starting dnstap-forwarder with HEC output..."
./dnstap-forwarder \
  -output-type hec \
  -hec-url http://localhost:8088/services/collector \
  -hec-token test-token \
  -batch-size 5 \
  -splunk-sourcetype dns_events \
  -splunk-host test-host \
  -log-level debug &
FORWARDER_PID=$!

echo "Waiting for forwarder to start..."
sleep 3

echo "Checking mock server stats..."
curl -s http://localhost:8088/stats | jq .

echo "Stopping forwarder..."
kill $FORWARDER_PID
sleep 2

echo "Final mock server stats..."
curl -s http://localhost:8088/stats | jq .

kill $MOCK_PID

echo
echo "=== All tests completed ===" 