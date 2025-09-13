#!/bin/bash

# Build script for Multithreaded Downloader
set -e

echo "🚀 Building Multithreaded Downloader..."
echo "====================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo -e "${RED}❌ Docker is not installed or not in PATH${NC}"
    echo "Please install Docker first: https://docs.docker.com/get-docker/"
    exit 1
fi

# Check if docker-compose is available
if ! command -v docker-compose &> /dev/null; then
    echo -e "${YELLOW}⚠️  docker-compose not found, using 'docker compose' instead${NC}"
    DOCKER_COMPOSE="docker compose"
else
    DOCKER_COMPOSE="docker-compose"
fi

# Function to display help
show_help() {
    echo "Usage: $0 [OPTION]"
    echo ""
    echo "Options:"
    echo "  build     Build the Docker image"
    echo "  start     Start the service with docker-compose"
    echo "  stop      Stop the service"
    echo "  restart   Restart the service"
    echo "  logs      Show service logs"
    echo "  clean     Clean up Docker resources"
    echo "  test      Run a quick test of the API"
    echo "  help      Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 build     # Build the image"
    echo "  $0 start     # Start the service"
    echo "  $0 test      # Test the API endpoints"
}

# Function to build Docker image
build_image() {
    echo -e "${BLUE}📦 Building Docker image...${NC}"
    docker build -t multithreaded-downloader:latest .
    echo -e "${GREEN}✅ Docker image built successfully!${NC}"
}

# Function to start service
start_service() {
    echo -e "${BLUE}🚀 Starting service with docker-compose...${NC}"
    $DOCKER_COMPOSE up -d
    echo -e "${GREEN}✅ Service started successfully!${NC}"
    echo -e "${BLUE}📊 Service status:${NC}"
    $DOCKER_COMPOSE ps
    echo ""
    echo -e "${BLUE}🌐 API available at: http://localhost:8080${NC}"
    echo -e "${BLUE}📋 Health check: http://localhost:8080/health${NC}"
}

# Function to stop service
stop_service() {
    echo -e "${BLUE}🛑 Stopping service...${NC}"
    $DOCKER_COMPOSE down
    echo -e "${GREEN}✅ Service stopped successfully!${NC}"
}

# Function to restart service
restart_service() {
    echo -e "${BLUE}🔄 Restarting service...${NC}"
    $DOCKER_COMPOSE restart
    echo -e "${GREEN}✅ Service restarted successfully!${NC}"
}

# Function to show logs
show_logs() {
    echo -e "${BLUE}📋 Showing service logs...${NC}"
    $DOCKER_COMPOSE logs -f
}

# Function to clean up
clean_up() {
    echo -e "${BLUE}🧹 Cleaning up Docker resources...${NC}"
    
    # Stop and remove containers
    $DOCKER_COMPOSE down --remove-orphans
    
    # Remove image
    docker rmi multithreaded-downloader:latest 2>/dev/null || true
    
    # Clean up unused Docker resources
    docker system prune -f
    
    echo -e "${GREEN}✅ Cleanup completed!${NC}"
}

# Function to test API
test_api() {
    echo -e "${BLUE}🧪 Testing API endpoints...${NC}"
    
    # Check if service is running
    if ! curl -s http://localhost:8080/health > /dev/null; then
        echo -e "${RED}❌ Service is not running. Start it first with: $0 start${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✅ Health check passed${NC}"
    
    # Test health endpoint
    echo -e "${BLUE}📊 Health endpoint:${NC}"
    curl -s http://localhost:8080/health | jq '.' || curl -s http://localhost:8080/health
    echo ""
    
    # Test stats endpoint
    echo -e "${BLUE}📈 Stats endpoint:${NC}"
    curl -s http://localhost:8080/stats | jq '.' || curl -s http://localhost:8080/stats
    echo ""
    
    # Test downloads list
    echo -e "${BLUE}📋 Downloads list:${NC}"
    curl -s http://localhost:8080/downloads | jq '.' || curl -s http://localhost:8080/downloads
    echo ""
    
    echo -e "${GREEN}✅ API tests completed!${NC}"
}

# Main script logic
case "${1:-help}" in
    build)
        build_image
        ;;
    start)
        start_service
        ;;
    stop)
        stop_service
        ;;
    restart)
        restart_service
        ;;
    logs)
        show_logs
        ;;
    clean)
        clean_up
        ;;
    test)
        test_api
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        echo -e "${RED}❌ Unknown option: $1${NC}"
        echo ""
        show_help
        exit 1
        ;;
esac
