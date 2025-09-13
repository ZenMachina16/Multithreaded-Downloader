#!/bin/bash

# Test script for the Redis queue-based multithreaded downloader
echo "Testing Redis Queue-Based Multithreaded Downloader"
echo "=================================================="

SERVER_URL="http://localhost:8080"
REDIS_URL="redis://localhost:6379"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}$1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Test 1: Health check
print_status "\n1. Testing health endpoint..."
HEALTH_RESPONSE=$(curl -s "$SERVER_URL/health")
if echo "$HEALTH_RESPONSE" | jq -e '.status == "healthy"' > /dev/null 2>&1; then
    print_success "Health check passed"
    echo "$HEALTH_RESPONSE" | jq '.'
else
    print_error "Health check failed"
    echo "$HEALTH_RESPONSE"
    exit 1
fi

# Test 2: Check initial queue stats
print_status "\n2. Checking initial queue statistics..."
curl -s "$SERVER_URL/queue/stats" | jq '.'

# Test 3: Enqueue multiple downloads
print_status "\n3. Enqueueing multiple download jobs..."

# Job 1: Small test file
JOB1_RESPONSE=$(curl -s -X POST "$SERVER_URL/downloads" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://httpbin.org/bytes/1048576",
    "output": "test_1mb.bin",
    "threads": 2
  }')

JOB1_ID=$(echo "$JOB1_RESPONSE" | jq -r '.job_id')
if [ "$JOB1_ID" != "null" ] && [ -n "$JOB1_ID" ]; then
    print_success "Job 1 enqueued: $JOB1_ID"
else
    print_error "Failed to enqueue Job 1"
    echo "$JOB1_RESPONSE"
fi

# Job 2: Another test file
JOB2_RESPONSE=$(curl -s -X POST "$SERVER_URL/downloads" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://httpbin.org/bytes/2097152",
    "output": "test_2mb.bin",
    "threads": 4
  }')

JOB2_ID=$(echo "$JOB2_RESPONSE" | jq -r '.job_id')
if [ "$JOB2_ID" != "null" ] && [ -n "$JOB2_ID" ]; then
    print_success "Job 2 enqueued: $JOB2_ID"
else
    print_error "Failed to enqueue Job 2"
    echo "$JOB2_RESPONSE"
fi

# Job 3: Third test file
JOB3_RESPONSE=$(curl -s -X POST "$SERVER_URL/downloads" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://httpbin.org/bytes/3145728",
    "output": "test_3mb.bin",
    "threads": 6
  }')

JOB3_ID=$(echo "$JOB3_RESPONSE" | jq -r '.job_id')
if [ "$JOB3_ID" != "null" ] && [ -n "$JOB3_ID" ]; then
    print_success "Job 3 enqueued: $JOB3_ID"
else
    print_error "Failed to enqueue Job 3"
    echo "$JOB3_RESPONSE"
fi

# Test 4: Check queue stats after enqueuing
print_status "\n4. Queue statistics after enqueuing jobs..."
curl -s "$SERVER_URL/queue/stats" | jq '.'

# Test 5: Monitor job progress
print_status "\n5. Monitoring job progress..."
for i in {1..30}; do
    echo "--- Check $i ---"
    
    # Check Job 1 status
    if [ -n "$JOB1_ID" ]; then
        JOB1_STATUS=$(curl -s "$SERVER_URL/downloads/$JOB1_ID/status")
        JOB1_STATE=$(echo "$JOB1_STATUS" | jq -r '.status')
        JOB1_PROGRESS=$(echo "$JOB1_STATUS" | jq -r '.progress')
        echo "Job 1: $JOB1_STATE (${JOB1_PROGRESS}%)"
    fi
    
    # Check Job 2 status
    if [ -n "$JOB2_ID" ]; then
        JOB2_STATUS=$(curl -s "$SERVER_URL/downloads/$JOB2_ID/status")
        JOB2_STATE=$(echo "$JOB2_STATUS" | jq -r '.status')
        JOB2_PROGRESS=$(echo "$JOB2_STATUS" | jq -r '.progress')
        echo "Job 2: $JOB2_STATE (${JOB2_PROGRESS}%)"
    fi
    
    # Check Job 3 status
    if [ -n "$JOB3_ID" ]; then
        JOB3_STATUS=$(curl -s "$SERVER_URL/downloads/$JOB3_ID/status")
        JOB3_STATE=$(echo "$JOB3_STATUS" | jq -r '.status')
        JOB3_PROGRESS=$(echo "$JOB3_STATUS" | jq -r '.progress')
        echo "Job 3: $JOB3_STATE (${JOB3_PROGRESS}%)"
    fi
    
    # Check if all jobs are completed or failed
    ALL_DONE=true
    for job_id in "$JOB1_ID" "$JOB2_ID" "$JOB3_ID"; do
        if [ -n "$job_id" ]; then
            STATUS=$(curl -s "$SERVER_URL/downloads/$job_id/status" | jq -r '.status')
            if [ "$STATUS" != "completed" ] && [ "$STATUS" != "failed" ]; then
                ALL_DONE=false
                break
            fi
        fi
    done
    
    if [ "$ALL_DONE" = true ]; then
        print_success "All jobs completed!"
        break
    fi
    
    sleep 3
done

# Test 6: Final status check
print_status "\n6. Final status check..."
if [ -n "$JOB1_ID" ]; then
    echo "Job 1 Final Status:"
    curl -s "$SERVER_URL/downloads/$JOB1_ID/status" | jq '.'
fi

if [ -n "$JOB2_ID" ]; then
    echo "Job 2 Final Status:"
    curl -s "$SERVER_URL/downloads/$JOB2_ID/status" | jq '.'
fi

if [ -n "$JOB3_ID" ]; then
    echo "Job 3 Final Status:"
    curl -s "$SERVER_URL/downloads/$JOB3_ID/status" | jq '.'
fi

# Test 7: List all downloads
print_status "\n7. Listing all downloads..."
curl -s "$SERVER_URL/downloads" | jq '.'

# Test 8: Final queue statistics
print_status "\n8. Final queue statistics..."
curl -s "$SERVER_URL/queue/stats" | jq '.'

# Test 9: Worker statistics (if available)
print_status "\n9. Worker statistics..."
curl -s "$SERVER_URL/workers/stats" | jq '.'

print_success "\nQueue system test completed!"

# Test 10: Performance summary
print_status "\n10. Performance Summary"
echo "================================"
echo "Jobs enqueued: 3"
echo "Total data: ~6MB"
echo "Workers: Multiple (check docker-compose logs)"
echo ""
echo "Check the following:"
echo "- Redis Commander: http://localhost:8081"
echo "- pgAdmin: http://localhost:8082 (admin@example.com / admin)"
echo "- API Health: $SERVER_URL/health"
echo "- Queue Stats: $SERVER_URL/queue/stats"

print_status "\nTo monitor the system:"
echo "docker-compose -f docker-compose-queue.yml logs -f worker-1"
echo "docker-compose -f docker-compose-queue.yml logs -f worker-2"
echo "docker-compose -f docker-compose-queue.yml logs -f worker-3"
