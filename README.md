# Load Balancer (Go)

A simple reverse-proxy load balancer written in Go. It routes HTTP traffic to a pool of backend servers, supports health checks with automatic failover, and can be containerized with Docker for easy deployment.

## Features
- Round-robin and least-connections routing
- Active health checks with automatic failover
- Graceful shutdown


## Architecture
```
Client -> Load Balancer (reverse proxy) -> Healthy backend servers
```
- The listener accepts HTTP requests.
- The router selects a healthy target using the chosen algorithm.
- Health checks continuously monitor backend status.
- Only healthy servers receive new requests.

## Quick Start

### Prerequisites
- Go 1.21 or higher  
- (Optional) Docker  

### Build
```bash
git clone https://github.com/KulinduC/Load-Balancer.git
cd Load-Balancer
go build -o load-balancer ./cmd/lb
```

## Docker

### Build and run
```bash
docker build -t load-balancer:latest .
docker run --rm -p 8080:8080 -v $(pwd)/configs:/app/configs load-balancer:latest \
  -config /app/configs/example.yaml
```

### Docker Compose example
```yaml
services:
  lb:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./configs:/app/configs
    command: ["-config", "/app/configs/example.yaml"]
```

## Health Checks
- Periodically sends `GET` requests to `{backend}/health`.
- A backend is marked unhealthy if it times out or returns a non-2xx status.
- Unhealthy backends are skipped until recovery.
