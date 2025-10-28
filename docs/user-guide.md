# Using the VPN Rotator CLI

This guide explains how to use the `vpn-rotator` command-line application to connect to your self-hosted VPN service.

## Prerequisites

Before you can use the CLI, you need to have [WireGuard](https://www.wireguard.com/install/) installed on your system. The CLI uses the `wg-quick` command to manage network interfaces.

- **macOS**: `brew install wireguard-tools`
- **Linux**: `sudo apt-get install wireguard-tools`
- **Windows**: Download and install from the [WireGuard website](https://www.wireguard.com/install/).

## Installation

1.  **Build the CLI from source**:

    Navigate to the `cmd/connector` directory in the project and run the build command:

    ```bash
    cd cmd/connector
    go build -o vpn-rotator
    ```

    Or use the Makefile from the project root:

    ```bash
    make build
    ```

2.  **Move the executable to your PATH** (optional but recommended):

    To make the `vpn-rotator` command available system-wide, move it to a directory in your system's `PATH`.

    ```bash
    sudo mv cmd/connector/vpn-rotator /usr/local/bin/
    ```

## Configuration

The VPN Rotator connector supports three ways to configure the application (in order of precedence):

1. **Command-line flags** (highest priority)
2. **Environment variables**
3. **Configuration file** (lowest priority)

For comprehensive configuration documentation including the rotator service, see the [Configuration Guide](configuration.md).

### Configuration File

The connector searches for `config.yaml` in the following locations:

1. `/etc/vpn-rotator/config.yaml`
2. `$HOME/.config/vpn-rotator/config.yaml`
3. `./config.yaml` (current directory)

You can also specify a custom config file location:

```bash
vpn-rotator connect --config /path/to/config.yaml
# OR
export VPN_ROTATOR_CONFIG_FILE=/path/to/config.yaml
```

**Example config.yaml:**

```yaml
# VPN Rotator Service API
api_url: http://your-rotator-service.com:8080

# WireGuard Configuration
key_path: ~/.vpn-rotator/client.key
interface: wg0
allowed_ips: 0.0.0.0/0
dns: 1.1.1.1,8.8.8.8

# Polling Configuration
poll_interval: 15  # minutes

# Retry Configuration
retry_attempts: 3
retry_delay: 5  # seconds

# Logging Configuration
log_level: info    # debug, info, warn, error
log_format: text   # text, json
```

### Environment Variables

All configuration options can be set via environment variables with the `VPN_ROTATOR_` prefix:

```bash
export VPN_ROTATOR_API_URL=http://your-rotator-service.com:8080
export VPN_ROTATOR_KEY_PATH=~/.vpn-rotator/client.key
export VPN_ROTATOR_INTERFACE=wg0
export VPN_ROTATOR_POLL_INTERVAL=15
export VPN_ROTATOR_ALLOWED_IPS=0.0.0.0/0
export VPN_ROTATOR_DNS=1.1.1.1,8.8.8.8
export VPN_ROTATOR_LOG_LEVEL=debug
export VPN_ROTATOR_LOG_FORMAT=json
export VPN_ROTATOR_RETRY_ATTEMPTS=3
export VPN_ROTATOR_RETRY_DELAY=5
```

Environment variables are particularly useful for containerized deployments or CI/CD environments.

## Usage

The `vpn-rotator` CLI requires `sudo` or root privileges to manage network interfaces.

### Connecting to the VPN

To connect to the VPN, use the `connect` command.

**Using configuration file:**

```bash
sudo vpn-rotator connect
```

**Using environment variables:**

```bash
export VPN_ROTATOR_API_URL=http://your-rotator-service.com:8080
sudo -E vpn-rotator connect
```

**Using command-line flags:**

```bash
sudo vpn-rotator connect --api-url http://your-rotator-service.com:8080
```

The CLI will automatically:
1. Fetch the latest VPN configuration from the rotator service
2. Generate a client key if one doesn't exist (and persist it for future connections)
3. Establish the WireGuard connection
4. Start polling for automatic rotation (default: every 15 minutes)

The connection process will run in the foreground and handle automatic rotations. Press `Ctrl+C` to disconnect gracefully.

### Checking the Connection Status

Use the `status` command to check if you are connected to the VPN.

```bash
sudo vpn-rotator status
```

This shows:
- Connection status
- Current WireGuard interface statistics
- Active peer information

### Disconnecting from the VPN

To disconnect, use the `disconnect` command.

```bash
sudo vpn-rotator disconnect
```

Or simply press `Ctrl+C` if the connector is running in the foreground.

### Available Flags

You can customize the behavior with the following flags (available for all commands):

**Global Flags:**
- `--config`: Path to configuration file
- `-v, --verbose`: Enable verbose (debug) logging

**Connect Command Flags:**
- `--api-url`: The URL of your VPN Rotator API service
- `--key-path`: Path to store your client's private key (default: `~/.vpn-rotator/client.key`)
- `--interface`: The name of the WireGuard network interface (default: `wg0`)
- `--poll-interval`: Minutes between rotation checks (default: `15`)
- `--allowed-ips`: The IP ranges to route through the VPN (default: `0.0.0.0/0`)
- `--dns`: DNS servers to use when connected (default: `1.1.1.1,8.8.8.8`)

## Logging

The connector supports structured logging with configurable levels and formats.

### Log Levels

- `debug`: Detailed diagnostic information
- `info`: General informational messages (default)
- `warn`: Warning messages
- `error`: Error messages

### Log Formats

- `text`: Human-readable text format (default)
- `json`: Structured JSON format (recommended for production)

**Example with debug logging:**

```bash
export VPN_ROTATOR_LOG_LEVEL=debug
export VPN_ROTATOR_LOG_FORMAT=text
sudo -E vpn-rotator connect
```

**Example with JSON logging (for production):**

```bash
export VPN_ROTATOR_LOG_LEVEL=info
export VPN_ROTATOR_LOG_FORMAT=json
sudo -E vpn-rotator connect
```

## Automatic Rotation

When connected, the connector automatically polls the rotator service for new VPN nodes. When a new node is provisioned:

1. The connector detects the new node
2. Gracefully disconnects from the current node
3. Generates new WireGuard configuration
4. Connects to the new node
5. Continues polling

This process is seamless and requires no user intervention.

## Troubleshooting

### Permission Denied

The connector requires root privileges to manage network interfaces:

```bash
# Correct
sudo vpn-rotator connect

# Incorrect - will fail
vpn-rotator connect
```

### WireGuard Tools Not Found

Ensure `wg-quick` is installed and in your PATH:

```bash
which wg-quick
# Should output: /usr/bin/wg-quick (or similar)
```

### Connection Fails

Enable debug logging to see detailed information:

```bash
export VPN_ROTATOR_LOG_LEVEL=debug
sudo -E vpn-rotator connect
```

### Configuration Not Found

Verify your configuration file location:

```bash
# Create config directory
mkdir -p ~/.config/vpn-rotator

# Create config file
cat > ~/.config/vpn-rotator/config.yaml <<EOF
api_url: http://your-rotator-service.com:8080
key_path: ~/.vpn-rotator/client.key
interface: wg0
log_level: debug
EOF
```

## Advanced Usage

### Running as a Systemd Service

See [deployment.md](deployment.md) for systemd service setup.

### Docker Deployment

The connector can run in a privileged Docker container:

```bash
docker run --rm --privileged \
  -e VPN_ROTATOR_API_URL=http://rotator:8080 \
  -e VPN_ROTATOR_LOG_LEVEL=info \
  -e VPN_ROTATOR_LOG_FORMAT=json \
  vpn-rotator-connector:latest
```

### Custom Polling Interval

Adjust the rotation check frequency:

```bash
# Check every 5 minutes
export VPN_ROTATOR_POLL_INTERVAL=5
sudo -E vpn-rotator connect
```

### Split Tunnel Configuration

Route only specific IPs through the VPN:

```bash
# Route only private networks
export VPN_ROTATOR_ALLOWED_IPS=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
sudo -E vpn-rotator connect
```
