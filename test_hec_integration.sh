#!/bin/bash

# Simple test script for HEC integration
# This demonstrates the mock server capabilities

set -e

echo "=== HEC Integration Test ==="
echo

# Function to cleanup
cleanup() {
    echo "Cleaning up..."
    pkill -f mock_hec_server || true
    pkill -f dnstap-forwarder || true
}

trap cleanup EXIT

# Test 1: Basic mock server functionality
echo "=== Test 1: Basic Mock Server ==="
./mock_hec_server/mock_hec_server -port 8090 -tokens "test-token" &
MOCK_PID=$!

sleep 2

echo "Testing health endpoint..."
curl -s http://localhost:8090/health | jq .

echo "Testing HEC endpoint..."
curl -s -X POST http://localhost:8090/services/collector \
  -H "Authorization: Splunk test-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

echo "Testing stats..."
curl -s http://localhost:8090/stats | jq .

kill $MOCK_PID
sleep 1

# Test 2: Simulate failures
echo
echo "=== Test 2: Simulate Failures ==="
./mock_hec_server/mock_hec_server -port 8090 -tokens "test-token" -response-code 500 &
MOCK_PID=$!

sleep 2

echo "Testing HEC endpoint with 500 response..."
curl -s -X POST http://localhost:8090/services/collector \
  -H "Authorization: Splunk test-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

echo "Testing stats..."
curl -s http://localhost:8090/stats | jq .

kill $MOCK_PID
sleep 1

# Test 3: Simulate slow responses
echo
echo "=== Test 3: Simulate Slow Responses ==="
./mock_hec_server/mock_hec_server -port 8090 -tokens "test-token" -response-delay 2s &
MOCK_PID=$!

sleep 2

echo "Testing HEC endpoint with delay (this will take 2 seconds)..."
time curl -s -X POST http://localhost:8090/services/collector \
  -H "Authorization: Splunk test-token" \
  -H "Content-Type: application/json" \
  -d '[{"event":{"test":"data"}}]' | jq .

kill $MOCK_PID

echo
echo "=== All tests completed successfully ===" 