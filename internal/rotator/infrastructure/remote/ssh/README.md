# SSH Infrastructure

This package provides SSH connectivity infrastructure for remote node operations.

## Components

### Client (`client.go`)

- **Purpose**: Basic SSH client implementation
- **Features**:
    - Command execution with context support
    - Connection health checking
    - Automatic reconnection on stale connections
    - Proper resource cleanup

### Pool (`pool.go`)

- **Purpose**: SSH connection pooling for efficient resource management
- **Features**:
    - Connection reuse and pooling
    - Automatic cleanup of idle connections
    - Retry logic with exponential backoff
    - Connection health monitoring
    - Comprehensive error handling
    - Statistics and monitoring

## Usage

### Basic Client Usage

```go
client, err := ssh.NewClient("192.168.1.100", "root", privateKey)
if err != nil {
    return err
}
defer client.Close()

output, err := client.RunCommand(ctx, "ls -la")
```

### Pool Usage (Recommended)

```go
pool := ssh.NewPool(privateKey, logger, 5*time.Minute)

// Execute command with automatic connection management
output, err := pool.ExecuteCommand(ctx, "192.168.1.100", "systemctl status wireguard")
```

## Integration with NodeInteractor

The SSH infrastructure is integrated with the NodeInteractor through the `SSHNodeInteractor` implementation:

1. **Connection Pooling**: Reuses SSH connections across multiple operations
2. **Circuit Breaker**: Provides resilience against failing nodes
3. **Retry Logic**: Automatically retries failed operations with backoff
4. **Monitoring**: Tracks connection statistics and health

## Configuration

SSH behavior is configured through the `NodeInteractorConfig`:

- `SSHTimeout`: Connection timeout
- `CommandTimeout`: Command execution timeout
- `RetryAttempts`: Number of retry attempts
- `RetryBackoff`: Backoff duration between retries

## Error Handling

The package provides comprehensive error handling:

- **Retryable Errors**: Network timeouts, connection refused, etc.
- **Non-retryable Errors**: Authentication failures, command not found, etc.
- **Circuit Breaker**: Prevents cascading failures

## Testing

Run tests with:

```bash
go test ./internal/rotator/infrastructure/remote/ssh/...
```

## Security Considerations

- Uses SSH key-based authentication
- Currently uses `InsecureIgnoreHostKey` for host key verification (TODO: implement proper verification)
- Connections are automatically closed after idle timeout
- Private keys are handled securely in memory