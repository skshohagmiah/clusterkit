# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY ../go.mod ../go.sum ./
RUN go mod download

# Copy source code
COPY .. .

# Build the application
WORKDIR /app/example
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kv-store .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/example/kv-store .

# Expose ports
# 8080-8082: ClusterKit HTTP (cluster coordination)
# 9001-9003: Raft (consensus)
# 9080-9082: KV Store HTTP (data operations)
EXPOSE 8080 9001 9080

# Create data directory
RUN mkdir -p /data

# Run the application
CMD ["./kv-store"]
