# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY ../../go.mod ../../go.sum ./
RUN go mod download

# Copy source code
COPY ../.. .

# Build the example application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o clusterkit-example ./example/main.go

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/clusterkit-example .

# Create data directory
RUN mkdir -p /data

# Expose HTTP and Raft ports
EXPOSE 8080 9001

# Run the example application
CMD ["./clusterkit-example"]
