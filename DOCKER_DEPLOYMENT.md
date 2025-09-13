# Docker Deployment Guide

## Quick Start

### Option 1: Docker Compose (Recommended)

```bash
# Build and start the service
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the service
docker-compose down
```

### Option 2: Docker Build & Run

```bash
# Build the image
docker build -t multithreaded-downloader .

# Run the container
docker run -d \
  --name downloader \
  -p 8080:8080 \
  -v downloader_data:/app/data \
  -v downloader_files:/app/downloads \
  multithreaded-downloader

# View logs
docker logs -f downloader
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GIN_MODE` | `release` | Gin framework mode (`debug`, `release`) |
| `DATABASE_PATH` | `/app/data/downloads.db` | SQLite database file path |
| `PORT` | `8080` | HTTP server port |

### Volumes

| Volume | Purpose | Recommended |
|--------|---------|-------------|
| `/app/data` | Database storage | **Required** for persistence |
| `/app/downloads` | Downloaded files | **Required** for file storage |

## Usage Examples

### 1. Start a Download

```bash
curl -X POST http://localhost:8080/downloads \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://releases.ubuntu.com/20.04/ubuntu-20.04.6-desktop-amd64.iso",
    "output": "ubuntu-20.04.6-desktop-amd64.iso",
    "threads": 8
  }'
```

### 2. Check Download Status

```bash
# Get download ID from previous response
DOWNLOAD_ID="your-download-id-here"
curl http://localhost:8080/downloads/$DOWNLOAD_ID/status
```

### 3. View All Downloads

```bash
curl http://localhost:8080/downloads
```

### 4. Get Statistics

```bash
curl http://localhost:8080/stats
```

## Production Deployment

### Docker Compose Production Setup

```yaml
version: '3.8'

services:
  downloader:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - /opt/downloader/data:/app/data
      - /opt/downloader/downloads:/app/downloads
    environment:
      - GIN_MODE=release
      - DATABASE_PATH=/app/data/downloads.db
    restart: unless-stopped
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: '2.0'
        reservations:
          memory: 256M
          cpus: '0.5'
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
    networks:
      - downloader_network

  # Optional: Reverse proxy
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - /etc/letsencrypt:/etc/letsencrypt:ro
    depends_on:
      - downloader
    restart: unless-stopped
    networks:
      - downloader_network

volumes:
  downloader_data:
    driver: local
  downloader_files:
    driver: local

networks:
  downloader_network:
    driver: bridge
```

### Nginx Configuration (nginx.conf)

```nginx
events {
    worker_connections 1024;
}

http {
    upstream downloader {
        server downloader:8080;
    }

    server {
        listen 80;
        server_name your-domain.com;

        location / {
            proxy_pass http://downloader;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            
            # For large file uploads
            client_max_body_size 0;
            proxy_read_timeout 300s;
            proxy_connect_timeout 75s;
        }
    }
}
```

## Monitoring & Management

### Health Checks

```bash
# Container health status
docker ps

# Application health endpoint
curl http://localhost:8080/health

# Detailed logs
docker logs downloader --tail 100 -f
```

### Database Management

```bash
# Access the container
docker exec -it downloader sh

# Or directly access SQLite database
docker exec -it downloader sqlite3 /app/data/downloads.db

# View downloads table
sqlite> .headers on
sqlite> .mode table
sqlite> SELECT * FROM downloads;
```

### Backup & Restore

```bash
# Backup database
docker exec downloader sqlite3 /app/data/downloads.db ".backup /app/data/backup.db"
docker cp downloader:/app/data/backup.db ./downloads_backup.db

# Restore database
docker cp ./downloads_backup.db downloader:/app/data/downloads.db
docker restart downloader
```

## Scaling & Performance

### Horizontal Scaling

For multiple instances, use a load balancer:

```yaml
version: '3.8'

services:
  downloader1:
    build: .
    volumes:
      - downloader1_data:/app/data
      - shared_downloads:/app/downloads
    networks:
      - downloader_network

  downloader2:
    build: .
    volumes:
      - downloader2_data:/app/data
      - shared_downloads:/app/downloads
    networks:
      - downloader_network

  nginx:
    image: nginx:alpine
    ports:
      - "8080:80"
    volumes:
      - ./nginx-lb.conf:/etc/nginx/nginx.conf:ro
    depends_on:
      - downloader1
      - downloader2
    networks:
      - downloader_network

volumes:
  downloader1_data:
  downloader2_data:
  shared_downloads:

networks:
  downloader_network:
```

### Resource Optimization

```yaml
# Resource limits for production
deploy:
  resources:
    limits:
      memory: 1G        # Adjust based on concurrent downloads
      cpus: '4.0'       # Adjust based on thread count
    reservations:
      memory: 512M
      cpus: '1.0'
```

## Troubleshooting

### Common Issues

1. **Container won't start**
   ```bash
   # Check logs
   docker logs downloader
   
   # Check if port is already in use
   netstat -tlnp | grep 8080
   ```

2. **Database permission errors**
   ```bash
   # Fix volume permissions
   docker exec -u root downloader chown -R appuser:appgroup /app/data
   ```

3. **Out of disk space**
   ```bash
   # Check Docker disk usage
   docker system df
   
   # Clean up unused images/containers
   docker system prune -a
   ```

4. **Downloads not persisting**
   ```bash
   # Verify volumes are mounted
   docker inspect downloader | grep -A 10 "Mounts"
   
   # Check database file exists
   docker exec downloader ls -la /app/data/
   ```

### Debug Mode

```bash
# Run in debug mode
docker run -it --rm \
  -p 8080:8080 \
  -v downloader_data:/app/data \
  -e GIN_MODE=debug \
  multithreaded-downloader
```

### Performance Monitoring

```bash
# Container resource usage
docker stats downloader

# Application metrics
curl http://localhost:8080/stats

# Database size
docker exec downloader du -sh /app/data/downloads.db
```

## Security Considerations

### Container Security

- Runs as non-root user (`appuser`)
- Minimal base image (Alpine Linux)
- Only necessary ports exposed
- Health checks for monitoring

### Network Security

```yaml
# Restrict container network access
networks:
  downloader_network:
    driver: bridge
    driver_opts:
      com.docker.network.bridge.name: br-downloader
    ipam:
      config:
        - subnet: 172.20.0.0/16
```

### File System Security

```bash
# Set proper permissions on host volumes
sudo mkdir -p /opt/downloader/{data,downloads}
sudo chown -R 1001:1001 /opt/downloader/
sudo chmod 755 /opt/downloader/
```

## Updates & Maintenance

### Update Application

```bash
# Pull latest code
git pull origin main

# Rebuild and restart
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Database Maintenance

```bash
# Vacuum database (reclaim space)
docker exec downloader sqlite3 /app/data/downloads.db "VACUUM;"

# Analyze database (update statistics)
docker exec downloader sqlite3 /app/data/downloads.db "ANALYZE;"
```

This Docker setup provides a production-ready deployment with persistence, health checks, and proper security practices! 🐳
