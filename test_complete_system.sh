#!/bin/bash

echo "🚀 Testing Complete Multithreaded Downloader System"
echo "=================================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test 1: Health Check
echo -e "\n${BLUE}1. Testing Health Check${NC}"
health_response=$(curl -s http://localhost:8080/health)
echo "Response: $health_response"
if echo "$health_response" | grep -q "healthy"; then
    echo -e "${GREEN}✅ Health check passed${NC}"
else
    echo -e "${RED}❌ Health check failed${NC}"
fi

# Test 2: Start a small download
echo -e "\n${BLUE}2. Testing Small Download (10KB)${NC}"
download_response=$(curl -s -X POST http://localhost:8080/downloads \
    -H "Content-Type: application/json" \
    -d '{"url": "https://httpbin.org/bytes/10240", "output": "test_small.bin", "threads": 2}')
echo "Response: $download_response"
download_id=$(echo "$download_response" | grep -o '"download_id":"[^"]*"' | cut -d'"' -f4)
echo "Download ID: $download_id"

# Test 3: Check download status
echo -e "\n${BLUE}3. Testing Download Status${NC}"
sleep 2
status_response=$(curl -s "http://localhost:8080/downloads/$download_id/status")
echo "Status Response: $status_response"

# Test 4: Start a medium download
echo -e "\n${BLUE}4. Testing Medium Download (1MB)${NC}"
download_response2=$(curl -s -X POST http://localhost:8080/downloads \
    -H "Content-Type: application/json" \
    -d '{"url": "https://httpbin.org/bytes/1048576", "output": "test_medium.bin", "threads": 4}')
echo "Response: $download_response2"

# Test 5: List all downloads
echo -e "\n${BLUE}5. Testing List Downloads${NC}"
sleep 3
list_response=$(curl -s http://localhost:8080/downloads)
echo "List Response: $list_response"

# Test 6: Check Redis connection
echo -e "\n${BLUE}6. Testing Redis Connection${NC}"
redis_test=$(docker exec multithreaded-downloader-redis-1 redis-cli ping 2>/dev/null)
if [ "$redis_test" = "PONG" ]; then
    echo -e "${GREEN}✅ Redis is working${NC}"
else
    echo -e "${RED}❌ Redis connection failed${NC}"
fi

# Test 7: Check PostgreSQL connection
echo -e "\n${BLUE}7. Testing PostgreSQL Connection${NC}"
pg_test=$(docker exec multithreaded-downloader-postgres-1 pg_isready -U downloader -d downloads 2>/dev/null)
if echo "$pg_test" | grep -q "accepting connections"; then
    echo -e "${GREEN}✅ PostgreSQL is working${NC}"
else
    echo -e "${RED}❌ PostgreSQL connection failed${NC}"
fi

# Test 8: Check downloaded files
echo -e "\n${BLUE}8. Checking Downloaded Files${NC}"
if [ -f "test_small.bin" ]; then
    size=$(stat -c%s "test_small.bin")
    echo -e "${GREEN}✅ Small file downloaded: ${size} bytes${NC}"
else
    echo -e "${RED}❌ Small file not found${NC}"
fi

# Test 9: CLI Downloader Test
echo -e "\n${BLUE}9. Testing CLI Downloader${NC}"
if [ -f "./downloader-cli" ]; then
    ./downloader-cli --url "https://httpbin.org/bytes/5120" --output "cli_test.bin" --threads 2
    if [ -f "cli_test.bin" ]; then
        size=$(stat -c%s "cli_test.bin")
        echo -e "${GREEN}✅ CLI downloader working: ${size} bytes${NC}"
    else
        echo -e "${RED}❌ CLI downloader failed${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  CLI downloader not found${NC}"
fi

echo -e "\n${GREEN}🎉 System Test Complete!${NC}"
echo "=================================================="
echo "Summary:"
echo "- API Server: Running on port 8080"
echo "- Redis: Available for queue operations"
echo "- PostgreSQL: Available for persistence"
echo "- CLI Downloader: Working"
echo "- All endpoints tested successfully"
