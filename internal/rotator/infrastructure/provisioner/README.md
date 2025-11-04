# Cloud Provisioner

The Cloud Provisioner package provides a unified interface for provisioning VPN nodes across different cloud providers.
It's designed to be used by the Node domain service for infrastructure provisioning.

## Architecture

The package follows a clean architecture pattern with:

- **Interface**: `CloudProvisioner` - Defines the contract for cloud provisioning
- **Implementations**: Provider-specific implementations (e.g., `HetznerProvisioner`)
- **Factory**: Creates provisioners based on configuration
- **Adapters**: Provide backward compatibility with existing interfaces

## Features

- **Multi-provider support**: Currently supports Hetzner Cloud, extensible for AWS, GCP, etc.
- **Cloud-init integration**: Automated WireGuard server setup via cloud-init
- **Health checking**: Validates node readiness after provisioning
- **Error handling**: Comprehensive error types with retry logic
- **Backward compatibility**: Adapters for existing provisioner interfaces

## Usage

### Basic Setup

```go
import (
    "github.com/chiquitav2/vpn-rotator/internal/rotator/infrastructure/cloudprovisioner"
)

// Create configuration
config := &cloudprovisioner.Config{
    Provider: cloudprovisioner.ProviderTypeHetzner,
    Hetzner: &cloudprovisioner.HetznerConfig{
        ServerType:   "cx11",
        Image:        "ubuntu-22.04", 
        Location:     "nbg1",
        SSHPublicKey: "/path/to/ssh/key.pub",
    },
}

// Create provisioner
provisioner, err := cloudprovisioner.NewCloudProvisioner(
    config, 
    "your-hetzner-api-token", 
    logger,
)
if err != nil {
    return err
}
```

### Provisioning a Node

```go
// Create provisioning configuration
provisionConfig := cloudprovisioner.ProvisioningConfig{
    Region:       "nbg1",
    InstanceType: "cx11",
    ImageID:      "ubuntu-22.04",
    Subnet:       subnet, // *net.IPNet
    Tags: map[string]string{
        "service": "vpn-rotator",
        "env":     "production",
    },
}

// Provision the node
node, err := provisioner.ProvisionNode(ctx, provisionConfig)
if err != nil {
    return err
}

fmt.Printf("Provisioned node: %s at %s\n", node.ServerID, node.IPAddress)
```

### Destroying a Node

```go
err := provisioner.DestroyNode(ctx, node.ServerID)
if err != nil {
    return err
}
```

### Getting Node Status

```go
status, err := provisioner.GetNodeStatus(ctx, node.ServerID)
if err != nil {
    return err
}

fmt.Printf("Node %s status: %s\n", status.ServerID, status.Status)
```

## Integration with Node Service

The cloud provisioner is designed to be injected into the Node domain services:

```go
// Create all dependencies
cloudProvisioner := // ... create as shown above
nodeInteractor := // ... create NodeInteractor
ipService := // ... create IP service
nodeRepository := // ... create Node repository

// Create node service
nodeService := node.NewService(
    nodeRepository,
    cloudProvisioner, // Inject here
    nodeInteractor,
    logger,
    nodeServiceConfig,
)

// Create provisioning service
provisioningService := node.NewProvisioningService(
    nodeService,
    nodeRepository,
    cloudProvisioner, // Inject here
    nodeInteractor,
    ipService,
    logger,
    provisioningConfig,
)
```

## Cloud-Init Configuration

The provisioner automatically generates cloud-init configuration that:

1. **Installs WireGuard**: Installs WireGuard and required tools
2. **Configures networking**: Sets up IP forwarding and firewall rules
3. **Generates keys**: Creates WireGuard private/public key pairs
4. **Configures interface**: Sets up WireGuard interface with subnet
5. **Enables service**: Starts and enables WireGuard service
6. **SSH access**: Configures SSH access with provided public keys

### Template Variables

The cloud-init template uses these variables:

- `ServerPrivateKey`: WireGuard private key (generated)
- `ServerPublicKey`: WireGuard public key (generated)
- `SSHPublicKeys`: Array of SSH public keys for access
- `SubnetCIDR`: Subnet CIDR (e.g., "10.8.1.0/24")
- `GatewayIP`: Gateway IP for the subnet (e.g., "10.8.1.1")

## Error Handling

The package provides specific error types:

### ProvisioningError

```go
type ProvisioningError struct {
    ServerID  string // Server ID if created
    Stage     string // Which stage failed
    Message   string // Human-readable message
    Err       error  // Underlying error
    Retryable bool   // Whether operation can be retried
}
```

### DestructionError

```go
type DestructionError struct {
    ServerID string // Server ID being destroyed
    Message  string // Human-readable message
    Err      error  // Underlying error
}
```

## Backward Compatibility

For existing code using the old provisioner interface:

```go
// Wrap new provisioner with adapter
oldProvisioner := cloudprovisioner.NewCloudProvisionerAdapter(cloudProvisioner)

// Use with existing code
node, err := oldProvisioner.ProvisionNodeWithSubnet(ctx, subnet)
```

## Extending for New Providers

To add a new cloud provider:

1. **Create provider implementation**:
   ```go
   type AWSProvisioner struct {
       // AWS-specific fields
   }
   
   func (a *AWSProvisioner) ProvisionNode(ctx context.Context, config ProvisioningConfig) (*ProvisionedNode, error) {
       // AWS-specific implementation
   }
   ```

2. **Add to factory**:
   ```go
   const ProviderTypeAWS ProviderType = "aws"
   
   // Add case in NewCloudProvisioner
   case ProviderTypeAWS:
       return NewAWSProvisioner(apiToken, config.AWS, logger)
   ```

3. **Add configuration**:
   ```go
   type AWSConfig struct {
       Region       string
       InstanceType string
       // ... other AWS-specific config
   }
   ```

## Health Checking

The provisioner performs comprehensive health checks:

1. **SSH connectivity**: Verifies SSH port (22) is accessible
2. **WireGuard port**: Verifies WireGuard UDP port (51820) is accessible
3. **Service status**: Checks that WireGuard service is running
4. **Retry logic**: Retries health checks with exponential backoff

## Security Considerations

- **Private keys**: WireGuard private keys are generated on the server and never transmitted
- **SSH access**: Only SSH key authentication is enabled, password auth is disabled
- **Firewall**: UFW is configured to only allow SSH (22) and WireGuard (51820) ports
- **Root access**: Root account is configured for SSH key access only

## Configuration Examples

### Development Environment

```go
config := &cloudprovisioner.Config{
    Provider: cloudprovisioner.ProviderTypeHetzner,
    Hetzner: &cloudprovisioner.HetznerConfig{
        ServerType:   "cx11",        // Smallest instance
        Image:        "ubuntu-22.04",
        Location:     "nbg1",        // Nuremberg
        SSHPublicKey: "~/.ssh/id_rsa.pub",
    },
}
```

### Production Environment

```go
config := &cloudprovisioner.Config{
    Provider: cloudprovisioner.ProviderTypeHetzner,
    Hetzner: &cloudprovisioner.HetznerConfig{
        ServerType:   "cx21",        // More powerful instance
        Image:        "ubuntu-22.04",
        Location:     "nbg1",
        SSHPublicKey: "/etc/ssh/vpn-rotator.pub", // Dedicated key
    },
}
```

## Monitoring and Observability

The provisioner provides extensive logging:

- **Structured logging**: Uses slog for structured logging
- **Operation tracking**: Logs all major operations with context
- **Error details**: Comprehensive error information
- **Performance metrics**: Duration tracking for operations

## Testing

The package includes comprehensive tests:

- **Unit tests**: Test individual components
- **Integration tests**: Test with real cloud providers (when configured)
- **Mock implementations**: For testing without real infrastructure

Run tests:

```bash
go test ./internal/rotator/infrastructure/cloudprovisioner/...
```