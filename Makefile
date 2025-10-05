# ClusterKit Makefile

.PHONY: all build test clean deps fmt vet lint examples help

# Default target
all: deps fmt vet build test

# Build the project
build:
	@echo "Building ClusterKit..."
	go build -v ./...

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Vet code
vet:
	@echo "Vetting code..."
	go vet ./...

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	golangci-lint run

# Clean build artifacts
clean:
	@echo "Cleaning..."
	go clean ./...
	rm -f examples/*/main
	rm -f examples/kv-store/kv-store

# Build examples
examples: examples-simple examples-kv

examples-simple:
	@echo "Building simple example..."
	cd examples/simple && go build -o simple main.go

examples-kv:
	@echo "Building KV store example..."
	cd examples/kv-store && go build -o kv-store main.go

# Run simple example (requires etcd)
run-simple: examples-simple
	@echo "Running simple example..."
	cd examples/simple && ./simple

# Run KV store example (requires etcd)
run-kv: examples-kv
	@echo "Running KV store example..."
	cd examples/kv-store && ./kv-store

# Start etcd for development
start-etcd:
	@echo "Starting etcd..."
	docker run -d --name clusterkit-etcd \
		-p 2379:2379 -p 2380:2380 \
		quay.io/coreos/etcd:v3.5.10 \
		/usr/local/bin/etcd \
		--data-dir=/etcd-data \
		--listen-client-urls http://0.0.0.0:2379 \
		--advertise-client-urls http://0.0.0.0:2379 \
		--listen-peer-urls http://0.0.0.0:2380 \
		--initial-advertise-peer-urls http://0.0.0.0:2380 \
		--initial-cluster default=http://0.0.0.0:2380 \
		--initial-cluster-token tkn \
		--initial-cluster-state new

# Stop etcd
stop-etcd:
	@echo "Stopping etcd..."
	docker stop clusterkit-etcd || true
	docker rm clusterkit-etcd || true

# Check if etcd is running
check-etcd:
	@echo "Checking etcd status..."
	curl -s http://localhost:2379/health || echo "etcd is not running"

# Help
help:
	@echo "ClusterKit Build Commands:"
	@echo "  all          - Run deps, fmt, vet, build, test"
	@echo "  build        - Build the project"
	@echo "  test         - Run tests"
	@echo "  deps         - Install dependencies"
	@echo "  fmt          - Format code"
	@echo "  vet          - Vet code"
	@echo "  lint         - Lint code (requires golangci-lint)"
	@echo "  clean        - Clean build artifacts"
	@echo "  examples     - Build all examples"
	@echo "  run-simple   - Run simple example"
	@echo "  run-kv       - Run KV store example"
	@echo "  start-etcd   - Start etcd in Docker"
	@echo "  stop-etcd    - Stop etcd Docker container"
	@echo "  check-etcd   - Check if etcd is running"
	@echo "  help         - Show this help"
