# --- Build stage ---
FROM golang:1.24.6-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git

# Copy go mod file (go.sum doesn't exist yet)
COPY go.mod ./
RUN go mod download

COPY . .
# Static binary; cgo off is enough
RUN CGO_ENABLED=0 go build -o main .

  FROM alpine:latest

# Install ca-certificates for HTTPS and wget for health checks
RUN apk --no-cache add ca-certificates tzdata wget

# Non-root user
RUN addgroup -g 1001 -S appgroup && adduser -u 1001 -S appuser -G appgroup
USER appuser
WORKDIR /app

COPY --from=builder /app/main .

# Expose load balancer port
EXPOSE 8090

# Expose backend server ports (8080-8089 for up to 10 servers)
EXPOSE 8080-8089

# Health check using wget
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8090/loadbalancer/ || exit 1

CMD ["./main"]
