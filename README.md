# VPN Rotator

VPN Rotator is a self-hosted system that automatically rotates WireGuard VPN nodes on Hetzner Cloud to enhance privacy and security. It provisions nodes on-demand, automatically destroys idle nodes to save costs, and handles seamless client migration during rotation.

## Features

- **Automatic Server Rotation**: Automatically provisions new VPN servers and decommissions old ones on a configurable schedule.
- **Dynamic Provisioning**: Dynamically selects server types and locations from Hetzner Cloud for better resilience.
- **Cost-Effective**: Automatically destroys idle nodes to save on cloud provider costs.
- **Health Checks**: Monitors the health of VPN nodes to ensure reliability.
- **RESTful API**: Provides standardized JSON API with middleware for logging, request tracing, and CORS.
- **Connector CLI**: A command-line client to easily connect to and switch between VPN nodes.
- **Stateful and Resilient**: Uses SQLite with migrations and connection pooling for robust state management.
- **Structured Logging**: Context-aware logging with request IDs and structured fields using slog.
- **Hybrid Configuration**: Supports both YAML files and environment variables for flexible deployment.

## Architecture

The system consists of two main components:

- **Rotator Service**: A long-running service that manages the entire lifecycle of VPN nodes. It includes:
    - An **API server** with standardized JSON responses, middleware for logging/tracing, and health endpoints.
    - A **scheduler** for rotating nodes and cleaning up old ones.
    - A **provisioner** to interact with the Hetzner Cloud API.
    - An **orchestrator** to control the state of the nodes and coordinate provisioning/rotation.
    - A **database layer** with migrations, connection pooling, and transaction support.
- **Connector CLI**: A client-side application that fetches the latest configuration from the rotator service and configures the local WireGuard interface.

For a more detailed overview of the architecture, see [docs/architecture-detailed.md](docs/architecture-detailed.md).

## Getting Started

### Prerequisites

- [Go](https://golang.org/doc/install) (version 1.22 or later)
- [Docker](https://docs.docker.com/get-docker/) (for containerized deployment)
- A [Hetzner Cloud](https://www.hetzner.com/cloud) account and API token.
- An SSH key added to your Hetzner Cloud project.

### Configuration

The service supports both YAML configuration files and environment variables. For detailed configuration instructions, see the [Configuration Guide](docs/configuration.md).

#### Quick Start

1. **Set required environment variables**:
    ```bash
    export VPN_ROTATOR_HETZNER_API_TOKEN="your-hetzner-token"
    export VPN_ROTATOR_HETZNER_SSH_KEY="~/.ssh/id_ed25519.pub"
    export VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH="~/.ssh/id_ed25519"
    ```

2. **Or create a configuration file**:
    ```yaml
    # config.yaml
    hetzner:
      api_token: "YOUR_HETZNER_API_TOKEN"
      ssh_key: "~/.ssh/id_ed25519.pub"
      ssh_private_key_path: "~/.ssh/id_ed25519"
    ```

For complete configuration options, examples, and best practices, see:
- [Configuration Guide](docs/configuration.md) - Comprehensive configuration documentation
- [Configuration Examples](docs/configuration-examples.md) - Ready-to-use configuration templates

### Running the Service

You can run the rotator service using Go directly or with Docker.

#### With Go

```bash
# Build and run
make build
./cmd/rotator/rotator

# Or run directly
go run cmd/rotator/main.go
```

#### With Docker

For detailed Docker setup instructions, see the [Docker Setup Guide](docs/docker-setup.md).

**Quick start**:

```bash
# Copy environment template
cp .env.template .env

# Edit .env with your Hetzner API token and SSH keys
# Then start the services
docker-compose up -d
```

## API Endpoints

### Health Check

```bash
GET /health
```

Returns service health status:

```json
{
  "success": true,
  "data": {
    "status": "healthy",
    "version": "1.0.0"
  }
}
```

### Get Latest VPN Configuration

```bash
GET /api/v1/config/latest
```

Returns the current active VPN node configuration:

```json
{
  "success": true,
  "data": {
    "server_public_key": "...",
    "server_ip": "1.2.3.4",
    "port": 51820
  }
}
```

If provisioning is in progress:

```json
{
  "status": "provisioning",
  "message": "VPN node is being provisioned",
  "estimated_wait_seconds": 120,
  "retry_after_seconds": 30
}
```

Error responses include request IDs for tracing:

```json
{
  "success": false,
  "error": {
    "code": "config_error",
    "message": "Failed to retrieve VPN configuration",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

## Usage

### Connector CLI

The `vpn-rotator` CLI tool allows you to connect to the VPN service.

- `connect`: Connects to the current active VPN node.
- `disconnect`: Disconnects from the VPN.
- `status`: Shows the current connection status.

To build the connector:

```bash
go build -o vpn-rotator cmd/connector/main.go
```

## Development

This project uses a `Makefile` to streamline common development tasks.

- `make build`: Build both the rotator and connector binaries.
- `make test`: Run all tests.
- `make lint`: Run the linter.
- `make fmt`: Format the code.
- `make sqlc-generate`: Generate Go code from SQL queries.
- `make docker-build`: Build the Docker images for the service and CLI.

### Development Tools

Install the required development tools:

```bash
make install-tools
```

