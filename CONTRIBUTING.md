# Contributing to ClusterKit

Thank you for your interest in contributing to ClusterKit! We welcome contributions from the community.

## 🚀 Getting Started

1. **Fork the repository**
   ```bash
   git clone https://github.com/skshohagmiah/clusterkit.git
   cd clusterkit
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Build the project**
   ```bash
   go build -v
   ```

4. **Run the example**
   ```bash
   cd example
   ./run-cluster.sh
   ```

## 🔧 Development Setup

### Prerequisites

- Go 1.19 or higher
- Docker (optional, for testing)
- Git

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...
```

### Running the Example Cluster

```bash
# Using shell script
cd example
./run-cluster.sh

# Using Docker
cd example/docker
docker-compose up
```

## 📝 How to Contribute

### Reporting Bugs

If you find a bug, please create an issue with:
- Clear description of the problem
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
- Relevant logs or error messages

### Suggesting Features

We welcome feature suggestions! Please create an issue with:
- Clear description of the feature
- Use case and benefits
- Proposed API (if applicable)
- Any implementation ideas

### Pull Requests

1. **Create a branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes**
   - Write clean, documented code
   - Follow Go best practices
   - Add tests for new features
   - Update documentation

3. **Test your changes**
   ```bash
   go test ./...
   go build -v
   ```

4. **Commit your changes**
   ```bash
   git add .
   git commit -m "feat: add your feature description"
   ```

5. **Push to your fork**
   ```bash
   git push origin feature/your-feature-name
   ```

6. **Create a Pull Request**
   - Provide a clear description
   - Reference any related issues
   - Explain the changes made

## 🎨 Code Style

### Go Code Style

- Follow standard Go formatting (`gofmt`)
- Use meaningful variable names
- Add comments for exported functions
- Keep functions small and focused
- Handle errors properly

Example:
```go
// GetNodesForKey returns all nodes (primary + replicas) that should store this key.
// This is the main method developers should use for data distribution.
func (ck *ClusterKit) GetNodesForKey(key string) ([]Node, error) {
    partition, err := ck.GetPartitionForKey(key)
    if err != nil {
        return nil, err
    }
    // ... implementation
}
```

### Commit Message Convention

We follow conventional commits:

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Adding tests
- `chore:` - Maintenance tasks

Examples:
```
feat: add GetNodesForKey helper method
fix: resolve race condition in partition assignment
docs: update API reference in README
refactor: simplify consensus manager initialization
test: add unit tests for partition rebalancing
```

## 🧪 Testing Guidelines

### Unit Tests

- Write tests for all new features
- Test edge cases and error conditions
- Use table-driven tests when appropriate

Example:
```go
func TestGetPartitionForKey(t *testing.T) {
    tests := []struct {
        name    string
        key     string
        want    string
        wantErr bool
    }{
        {"valid key", "user:123", "partition-5", false},
        {"empty key", "", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Integration Tests

- Test multi-node scenarios
- Verify Raft consensus
- Test partition rebalancing
- Test node join/leave

## 📚 Documentation

### Code Documentation

- Add godoc comments for all exported types and functions
- Include usage examples in comments
- Document parameters and return values

### README Updates

- Update API reference for new features
- Add examples for new functionality
- Update environment variables table

### Example Updates

- Add examples for new features
- Update existing examples if API changes
- Ensure examples compile and run

## 🔍 Code Review Process

1. **Automated Checks**
   - Code must compile
   - Tests must pass
   - No race conditions

2. **Manual Review**
   - Code quality and style
   - Test coverage
   - Documentation completeness
   - API design

3. **Feedback**
   - Address review comments
   - Update PR as needed
   - Discuss design decisions

## 🎯 Areas for Contribution

### High Priority

- [ ] Additional unit tests
- [ ] Integration tests
- [ ] Performance benchmarks
- [ ] Documentation improvements
- [ ] Example applications

### Features

- [ ] Metrics and monitoring
- [ ] Admin UI/dashboard
- [ ] Additional partition strategies
- [ ] Enhanced health checks
- [ ] Backup and restore utilities

### Nice to Have

- [ ] gRPC support
- [ ] TLS/encryption
- [ ] Authentication/authorization
- [ ] Multi-datacenter support
- [ ] Dynamic configuration

## 💬 Communication

- **Issues** - For bugs and feature requests
- **Pull Requests** - For code contributions
- **Discussions** - For questions and ideas

## 📜 License

By contributing to ClusterKit, you agree that your contributions will be licensed under the MIT License.

## 🙏 Recognition

All contributors will be recognized in the project. Thank you for helping make ClusterKit better!

---

**Questions?** Feel free to open an issue or start a discussion!
