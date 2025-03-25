# Build stage for Go proxy
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the proxy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o proxy .

# Final stage using official imgproxy
FROM ghcr.io/imgproxy/imgproxy:v3.27.2

# Copy our proxy binary
COPY --from=builder /app/proxy /usr/local/bin/
COPY start_processes.sh /usr/local/bin/

EXPOSE 8080

# Start proxy (which will start tigris-proxy & imgproxy)
CMD ["/usr/local/bin/start_processes.sh"]
