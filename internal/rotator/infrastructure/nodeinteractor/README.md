# NodeInteractor

The NodeInteractor package provides a unified interface for all SSH server operations and remote node management in the
VPN rotator system. It abstracts SSH operations, WireGuard management, health checking, and system operations behind a
clean interface with comprehensive error handling, retry logic, and circuit breaker patterns.

## Features

- **Unified Interface**: Single interface for all node operations
- **Connection Pooling**: Efficient SSH connection management with pooling
- **Circuit Breaker**: Automatic failure detection and recovery for unreliable connections
- **Comprehensive Health Checking**: System metrics collection (CPU, memory, disk, network)
- **WireGuard Management**: Complete peer lifecycle management (add, remove, list, update, sync)
- **Retry Logic**: Exponential backoff retry logic with configurable attempts
- **Structured Logging**: Comprehensive logging with operation context and timing
- **Metrics Collection**: Operation success rates, response times, and error categorization
- **Error Handling**: Domain-specific error types with proper error wrapping

## Usage

### Basic Usage

```go
import (
    "context"
    "log/slog"
    "github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/nodeinteractor"
)

// Create with default configuration
interactor, err := nodeinteractor.NewNodeInteractorWithDefaults(sshPrivateKey, logger)
if err != nil {
    return err
}
defer interactor.Close()

// Check node health
health, err := interactor.CheckNodeHealth(ctx, "192.168.1.100")
if err != nil {
    return err
}

// Add a peer
peerConfig := nodeinteractor.PeerWireGuardConfig{
    PublicKey:  "peer_public_key",
    AllowedIPs: []string{"10.0.0.2/32"},
}
err = interactor.AddPeer(ctx, "192.168.1.100", peerConfig)
```

### Custom Configuration

```go
config := nodeinteractor.DefaultConfig()
config.SSHTimeout = 45 * time.Second
config.RetryAttempts = 5
config.CircuitBreaker.FailureThreshold = 10

interactor, err := nodeinteractor.NewEnhancedSSHNodeInteractor(sshPrivateKey, config, logger)
```

## Interface Methods

### Health Operations

- `CheckNodeHealth(ctx, nodeHost)` - Comprehensive health check with system metrics
- `GetNodeSystemInfo(ctx, nodeHost)` - Detailed system information

### WireGuard Operations

- `GetWireGuardStatus(ctx, nodeHost)` - Interface status and configuration
- `AddPeer(ctx, nodeHost, config)` - Add peer with validation and rollback
- `RemovePeer(ctx, nodeHost, publicKey)` - Remove peer with existence checking
- `UpdatePeer(ctx, nodeHost, config)` - Update peer configuration
- `ListPeers(ctx, nodeHost)` - List all peers with status
- `SyncPeers(ctx, nodeHost, configs)` - Synchronize to desired state

### Configuration Management

- `UpdateWireGuardConfig(ctx, nodeHost, config)` - Update complete configuration
- `RestartWireGuard(ctx, nodeHost)` - Restart WireGuard service
- `SaveWireGuardConfig(ctx, nodeHost)` - Persist configuration

### System Operations

- `ExecuteCommand(ctx, nodeHost, command)` - Execute arbitrary commands
- `UploadFile(ctx, nodeHost, localPath, remotePath)` - File upload
- `DownloadFile(ctx, nodeHost, remotePath, localPath)` - File download

## Error Types

The package provides comprehensive error types for different failure scenarios:

- `SSHConnectionError` - SSH connection failures
- `SSHTimeoutError` - SSH operation timeouts
- `SSHCommandError` - Command execution failures
- `WireGuardOperationError` - WireGuard operation failures
- `WireGuardConfigError` - Configuration errors
- `HealthCheckError` - Health check failures
- `SystemInfoError` - System information retrieval failures
- `FileOperationError` - File upload/download failures
- `CircuitBreakerError` - Circuit breaker state errors
- `ValidationError` - Configuration validation failures
- `PeerNotFoundError` - Peer not found errors
- `PeerAlreadyExistsError` - Peer already exists errors
- `IPConflictError` - IP address conflicts

## Configuration

The `NodeInteractorConfig` struct provides comprehensive configuration options:

```go
type NodeInteractorConfig struct {
    SSHTimeout          time.Duration        // SSH connection timeout
    CommandTimeout      time.Duration        // Command execution timeout
    WireGuardConfigPath string               // Path to WireGuard config file
    WireGuardInterface  string               // WireGuard interface name
    HealthCheckCommands []HealthCommand      // Custom health check commands
    RetryAttempts       int                  // Number of retry attempts
    RetryBackoff        time.Duration        // Retry backoff duration
    SystemMetricsPath   SystemMetricsPaths   // Paths for system metrics
    CircuitBreaker      CircuitBreakerConfig // Circuit breaker configuration
}
```

## Monitoring and Metrics

The enhanced implementation provides comprehensive metrics:

- Operation counts and success rates
- Average response times
- Error categorization
- Connection health per host
- Circuit breaker statistics

Access metrics via:

```go
enhanced := interactor.(*nodeinteractor.EnhancedSSHNodeInteractor)
metrics := enhanced.GetMetrics()
```

## Testing

For testing, use the testing-specific constructor:

```go
interactor, err := nodeinteractor.NewNodeInteractorForTesting(sshPrivateKey, logger)
```

This creates an instance with reduced timeouts and thresholds suitable for unit tests.

## Architecture Benefits

1. **Separation of Concerns**: Domain services only depend on NodeInteractor, not SSH details
2. **Testability**: Single interface to mock for testing node operations
3. **Reliability**: Built-in retry logic, circuit breakers, and error recovery
4. **Observability**: Comprehensive logging and metrics collection
5. **Maintainability**: Centralized SSH operations with consistent error handling