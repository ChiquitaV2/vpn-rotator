# VPN Rotator

VPN Rotator is a self-hosted system that automatically rotates WireGuard VPN nodes on Hetzner Cloud to enhance privacy and security. It provisions nodes on-demand, automatically destroys idle nodes to save costs, and handles seamless client migration during rotation.

## Features

- **Comprehensive Peer Management**: Full lifecycle management of WireGuard peers with automatic IP allocation and node
  assignment.
- **Race Condition Protection**: Prevents multiple nodes from being provisioned simultaneously during concurrent
  requests.
- **Automatic Server Rotation**: Automatically provisions new VPN servers and decommissions old ones with seamless peer
  migration.
- **Dynamic Provisioning**: Dynamically selects server types and locations from Hetzner Cloud for better resilience.
- **Intelligent Load Balancing**: Automatically distributes peers across nodes based on capacity and performance.
- **Cost-Effective**: Automatically destroys idle nodes to save on cloud provider costs.
- **Health Checks**: Monitors the health of VPN nodes to ensure reliability.
- **RESTful API**: Comprehensive JSON API for peer management with middleware for logging, request tracing, and CORS.
- **Connector CLI**: A command-line client to easily connect to and switch between VPN nodes.
- **Stateful and Resilient**: Uses SQLite with migrations, connection pooling, and circuit breakers for robust state
  management.
- **Structured Logging**: Context-aware logging with request IDs and structured fields using slog.
- **Hybrid Configuration**: Supports both YAML files and environment variables for flexible deployment.
- **IP Subnet Management**: Automatic subnet allocation and IP address management per node with conflict prevention.

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

### Peer Management

#### Connect to VPN

```bash
POST /api/v1/connect
```

Connect a peer to the VPN with automatic node selection and IP allocation:

**Request:**

```json
{
  "public_key": "client_public_key_here",
  "generate_keys": false
}
```

Or request server-side key generation:

```json
{
  "generate_keys": true
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "peer_id": "peer-uuid",
    "server_public_key": "server_public_key_here",
    "server_ip": "1.2.3.4",
    "server_port": 51820,
    "client_ip": "10.8.1.5",
    "client_private_key": "private_key_if_generated",
    "dns": ["1.1.1.1", "8.8.8.8"],
    "allowed_ips": ["0.0.0.0/0"]
  }
}
```

#### Disconnect from VPN

```bash
DELETE /api/v1/disconnect
```

**Request:**

```json
{
  "peer_id": "peer-uuid"
}
```

**Response:**

```json
{
  "success": true,
  "data": {
    "message": "Peer disconnected successfully",
    "peer_id": "peer-uuid"
  }
}
```

#### List Peers

```bash
GET /api/v1/peers?node_id=node1&status=active&limit=50&offset=0
```

**Response:**

```json
{
  "success": true,
  "data": {
    "peers": [
      {
        "id": "peer-uuid",
        "node_id": "node-uuid",
        "public_key": "peer_public_key",
        "allocated_ip": "10.8.1.5",
        "status": "active",
        "created_at": "2023-01-01T00:00:00Z",
        "last_handshake_at": "2023-01-01T12:00:00Z"
      }
    ],
    "total_count": 1,
    "offset": 0,
    "limit": 50
  }
}
```

#### Get Peer Details

```bash
GET /api/v1/peers/{peer_id}
```

**Response:**

```json
{
  "success": true,
  "data": {
    "id": "peer-uuid",
    "node_id": "node-uuid",
    "public_key": "peer_public_key",
    "allocated_ip": "10.8.1.5",
    "status": "active",
    "created_at": "2023-01-01T00:00:00Z",
    "last_handshake_at": "2023-01-01T12:00:00Z"
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

### Error Responses

All error responses include request IDs for tracing:

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

