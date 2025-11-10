# Getting Started

This guide will walk you through setting up your development environment for the VPN Rotator project.

## Prerequisites

* Go 1.25
* Docker and Docker Compose
* A Hetzner Cloud account and API token
* An SSH key pair added to your Hetzner Cloud account

## 1. Clone the Repository

```bash
git clone https://github.com/chiquitav2/vpn-rotator.git
cd vpn-rotator
```

## 2. Configuration

Copy the example configuration file:

```bash
cp config.example.yaml config.yaml
```

Now, edit `config.yaml` with your specific settings. The most important settings to configure are:

* `hetzner.api_token`: Your Hetzner Cloud API token.
* `hetzner.ssh_key`: The path to your SSH public key.
* `hetzner.ssh_private_key_path`: The path to your SSH private key.

For more details on configuration, see the [Configuration Guide](configuration.md).

## 3. Build and Run the Rotator Service

You can run the rotator service using Docker Compose, which will also start a local instance of the service.

```bash
docker-compose up --build rotator
```

To run the service directly without Docker:

```bash
go build -o vpn-rotator-server ./cmd/rotator
./vpn-rotator-server
```

## 4. Build and Run the Connector Client

In a separate terminal, build the connector client:

```bash
go build -o vpn-rotator-connector ./cmd/connector
```

Now, you can use the connector to interact with the rotator service. First, set up the connector's configuration. Create
a `connector.yaml` file:

```yaml
api_url: "http://localhost:8080"
key_path: "~/.vpn-rotator/client.key"
```

Then, run the `connect` command:

```bash
./vpn-rotator-connector --config connector.yaml connect
```

This will connect your device to the VPN. If no node is active, the rotator service will automatically provision one for
you.

## 5. Development

### Running Tests

To run the test suite:

```bash
go test ./...
```

### Database

The project uses `sqlc` to generate type-safe Go code from SQL queries. To regenerate the database code after changing
the queries in `db/queries.sql` or the schema in `db/schema.sql`:

```bash
sqlc generate
```
