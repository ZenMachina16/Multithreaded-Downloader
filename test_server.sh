#!/bin/bash

# Test script for the Multithreaded Downloader REST API
echo "Testing Multithreaded Downloader REST API Server"
echo "================================================"

SERVER_URL="http://localhost:8080"

# Test 1: Health check
echo -e "\n1. Testing health endpoint..."
curl -s "$SERVER_URL/health" | jq '.' || echo "Health check failed"

# Test 2: Start a download
echo -e "\n2. Starting a new download..."
DOWNLOAD_RESPONSE=$(curl -s -X POST "$SERVER_URL/downloads" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://httpbin.org/bytes/1048576",
    "output": "test_download.bin",
    "threads": 2
  }')

echo "$DOWNLOAD_RESPONSE" | jq '.'
DOWNLOAD_ID=$(echo "$DOWNLOAD_RESPONSE" | jq -r '.download_id')

if [ "$DOWNLOAD_ID" = "null" ] || [ -z "$DOWNLOAD_ID" ]; then
  echo "Failed to start download"
  exit 1
fi

echo "Download ID: $DOWNLOAD_ID"

# Test 3: Check status
echo -e "\n3. Checking download status..."
sleep 2
curl -s "$SERVER_URL/downloads/$DOWNLOAD_ID/status" | jq '.'

# Test 4: List all downloads
echo -e "\n4. Listing all downloads..."
curl -s "$SERVER_URL/downloads" | jq '.'

# Test 5: Pause download (if still running)
echo -e "\n5. Attempting to pause download..."
curl -s -X POST "$SERVER_URL/downloads/$DOWNLOAD_ID/pause" | jq '.'

# Test 6: Check status after pause
echo -e "\n6. Checking status after pause..."
sleep 1
curl -s "$SERVER_URL/downloads/$DOWNLOAD_ID/status" | jq '.'

# Test 7: Resume download
echo -e "\n7. Resuming download..."
curl -s -X POST "$SERVER_URL/downloads/$DOWNLOAD_ID/resume" | jq '.'

# Test 8: Final status check
echo -e "\n8. Final status check..."
sleep 3
curl -s "$SERVER_URL/downloads/$DOWNLOAD_ID/status" | jq '.'

# Test 9: Clean up
echo -e "\n9. Cleaning up download..."
curl -s -X DELETE "$SERVER_URL/downloads/$DOWNLOAD_ID" | jq '.'

echo -e "\nTest completed!"
