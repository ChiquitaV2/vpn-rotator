# Configuration Guide

This guide provides comprehensive configuration instructions for both the VPN Rotator service and the connector client.

## Table of Contents

- [Rotator Service Configuration](#rotator-service-configuration)
- [Connector Configuration](#connector-configuration)
- [Environment Variables Reference](#environment-variables-reference)
- [Configuration Examples](#configuration-examples)
- [Security Considerations](#security-considerations)
- [Troubleshooting](#troubleshooting)

## Rotator Service Configuration

The VPN Rotator service manages WireGuard VPN nodes on Hetzner Cloud. It supports both YAML configuration files and environment variables.

### Configuration File Locations

The service searches for `config.yaml` in the following order:

1. `/etc/vpn-rotator/config.yaml` (system-wide)
2. `$HOME/.vpn-rotator/config.yaml` (user-specific)
3. `./config.yaml` (current directory)
4. Custom path via `--config` flag

### Basic Configuration

Create a `config.yaml` file with the following structure:

```yaml
# Logging configuration
log:
  level: "info"        # debug, info, warn, error
  format: "text"       # text, json

# API server configuration
api:
  listen_addr: ":8080" # Address and port to bind the API server

# Database configuration
db:
  path: "./data/rotator.db"    # SQLite database file path
  max_open_conns: 25           # Maximum open database connections
  max_idle_conns: 5            # Maximum idle database connections
  conn_max_lifetime: 300       # Connection lifetime in seconds

# Service configuration
service:
  shutdown_timeout: "30s"      # Graceful shutdown timeout

# Scheduler configuration
scheduler:
  rotation_interval: "24h"     # How often to rotate VPN nodes
  cleanup_interval: "1h"       # How often to clean up old nodes
  cleanup_age: "2h"           # Age threshold for node cleanup

# Hetzner Cloud provider configuration
hetzner:
  api_token: "YOUR_HETZNER_API_TOKEN"           # Required: Hetzner Cloud API token
  server_types: ["cx11", "cx21"]                # Server types to use
  image: "ubuntu-22.04"                         # OS image for VPN nodes
  locations: ["nbg1", "fsn1", "hel1"]         # Datacenter locations
  ssh_key: "/path/to/your/ssh/public/key.pub"  # SSH public key file
  ssh_private_key_path: "/path/to/your/ssh/private/key" # SSH private key file
```

### Required Configuration

The following settings are **required** for the service to function:

1. **Hetzner API Token**: Get this from your [Hetzner Cloud Console](https://console.hetzner.cloud/)
2. **SSH Keys**: Both public and private SSH keys are required for node management

### SSH Key Setup

1. **Generate SSH keys** (if you don't have them):
   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/vpn-rotator -C "vpn-rotator@$(hostname)"
   ```

2. **Add public key to Hetzner Cloud**:
   - Go to [Hetzner Cloud Console](https://console.hetzner.cloud/)
   - Navigate to "SSH Keys" section
   - Add your public key (`~/.ssh/vpn-rotator.pub`)

3. **Configure paths in config.yaml**:
   ```yaml
   hetzner:
     ssh_key: "~/.ssh/vpn-rotator.pub"
     ssh_private_key_path: "~/.ssh/vpn-rotator"
   ```

### Advanced Configuration Options

#### Server Types and Locations

Choose server types and locations based on your needs:

```yaml
hetzner:
  # Available server types (as of 2024)
  server_types: 
    - "cx11"    # 1 vCPU, 4GB RAM, €3.29/month
    - "cx21"    # 2 vCPU, 8GB RAM, €5.83/month
    - "cx31"    # 2 vCPU, 16GB RAM, €10.52/month
  
  # Available locations
  locations:
    - "nbg1"    # Nuremberg, Germany
    - "fsn1"    # Falkenstein, Germany
    - "hel1"    # Helsinki, Finland
    - "ash"     # Ashburn, USA
    - "hil"     # Hillsboro, USA
```

#### Rotation and Cleanup Settings

```yaml
scheduler:
  rotation_interval: "12h"     # Rotate every 12 hours
  cleanup_interval: "30m"      # Check for cleanup every 30 minutes
  cleanup_age: "1h"           # Clean up nodes older than 1 hour
```

#### Database Tuning

```yaml
db:
  path: "./data/rotator.db"
  max_open_conns: 50          # Increase for high load
  max_idle_conns: 10          # Increase for better performance
  conn_max_lifetime: 600      # 10 minutes
```

## Connector Configuration

The connector client fetches VPN configurations from the rotator service and manages local WireGuard connections.

### Configuration File Locations

The connector searches for `config.yaml` in:

1. `/etc/vpn-rotator/config.yaml`
2. `$HOME/.config/vpn-rotator/config.yaml`
3. `./config.yaml`
4. Custom path via `--config` flag

### Basic Connector Configuration

```yaml
# VPN Rotator Service API
api_url: "http://your-rotator-service.com:8080"

# WireGuard Configuration
key_path: "~/.vpn-rotator/client.key"    # Client private key location
interface: "wg0"                          # WireGuard interface name
allowed_ips: "0.0.0.0/0"                # Traffic to route through VPN
dns: "1.1.1.1,8.8.8.8"                  # DNS servers to use

# Polling Configuration
poll_interval: 15                         # Minutes between rotation checks

# Retry Configuration
retry_attempts: 3                         # Number of retry attempts
retry_delay: 5                           # Seconds between retries

# Logging Configuration
log_level: "info"                        # debug, info, warn, error
log_format: "text"                       # text, json
```

### Advanced Connector Options

#### Split Tunneling

Route only specific traffic through the VPN:

```yaml
# Route only private networks
allowed_ips: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

# Route only specific services
allowed_ips: "1.1.1.1/32,8.8.8.8/32"
```

#### Custom DNS Configuration

```yaml
# Use custom DNS servers
dns: "9.9.9.9,149.112.112.112"  # Quad9 DNS

# Use local DNS server
dns: "192.168.1.1"

# Disable DNS override (use system DNS)
dns: ""
```

#### Multiple Interfaces

Run multiple connector instances with different interfaces:

```yaml
# Instance 1
interface: "wg0"
key_path: "~/.vpn-rotator/client1.key"

# Instance 2 (separate config file)
interface: "wg1"
key_path: "~/.vpn-rotator/client2.key"
```

## Environment Variables Reference

Both the rotator service and connector support environment variables with specific prefixes.

### Rotator Service Environment Variables

All variables use the `VPN_ROTATOR_` prefix:

```bash
# Required
export VPN_ROTATOR_HETZNER_API_TOKEN="your-hetzner-token"
export VPN_ROTATOR_HETZNER_SSH_KEY="/path/to/public/key.pub"
export VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH="/path/to/private/key"

# Optional - Logging
export VPN_ROTATOR_LOG_LEVEL="info"
export VPN_ROTATOR_LOG_FORMAT="json"

# Optional - API
export VPN_ROTATOR_API_LISTEN_ADDR=":8080"

# Optional - Database
export VPN_ROTATOR_DB_PATH="./data/rotator.db"
export VPN_ROTATOR_DB_MAX_OPEN_CONNS="25"
export VPN_ROTATOR_DB_MAX_IDLE_CONNS="5"
export VPN_ROTATOR_DB_CONN_MAX_LIFETIME="300"

# Optional - Service
export VPN_ROTATOR_SERVICE_SHUTDOWN_TIMEOUT="30s"

# Optional - Scheduler
export VPN_ROTATOR_SCHEDULER_ROTATION_INTERVAL="24h"
export VPN_ROTATOR_SCHEDULER_CLEANUP_INTERVAL="1h"
export VPN_ROTATOR_SCHEDULER_CLEANUP_AGE="2h"

# Optional - Hetzner
export VPN_ROTATOR_HETZNER_SERVER_TYPES="cx11,cx21"
export VPN_ROTATOR_HETZNER_IMAGE="ubuntu-22.04"
export VPN_ROTATOR_HETZNER_LOCATIONS="nbg1,fsn1"
```

### Connector Environment Variables

All variables use the `VPN_ROTATOR_` prefix:

```bash
# Required
export VPN_ROTATOR_API_URL="http://your-rotator-service.com:8080"

# Optional - WireGuard
export VPN_ROTATOR_KEY_PATH="~/.vpn-rotator/client.key"
export VPN_ROTATOR_INTERFACE="wg0"
export VPN_ROTATOR_ALLOWED_IPS="0.0.0.0/0"
export VPN_ROTATOR_DNS="1.1.1.1,8.8.8.8"

# Optional - Polling
export VPN_ROTATOR_POLL_INTERVAL="15"

# Optional - Retry
export VPN_ROTATOR_RETRY_ATTEMPTS="3"
export VPN_ROTATOR_RETRY_DELAY="5"

# Optional - Logging
export VPN_ROTATOR_LOG_LEVEL="info"
export VPN_ROTATOR_LOG_FORMAT="text"
```

## Configuration Examples

### Production Rotator Service

```yaml
# /etc/vpn-rotator/config.yaml
log:
  level: "info"
  format: "json"

api:
  listen_addr: ":8080"

db:
  path: "/var/lib/vpn-rotator/rotator.db"
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 600

service:
  shutdown_timeout: "30s"

scheduler:
  rotation_interval: "24h"
  cleanup_interval: "30m"
  cleanup_age: "2h"

hetzner:
  api_token: "${HETZNER_API_TOKEN}"
  server_types: ["cx11", "cx21"]
  image: "ubuntu-22.04"
  locations: ["nbg1", "fsn1", "hel1"]
  ssh_key: "/etc/vpn-rotator/ssh/id_ed25519.pub"
  ssh_private_key_path: "/etc/vpn-rotator/ssh/id_ed25519"
```

### Development Rotator Service

```yaml
# ./config.yaml
log:
  level: "debug"
  format: "text"

api:
  listen_addr: ":8080"

db:
  path: "./data/rotator.db"

scheduler:
  rotation_interval: "1h"    # Faster rotation for testing
  cleanup_interval: "5m"     # Frequent cleanup
  cleanup_age: "10m"

hetzner:
  api_token: "your-dev-token"
  server_types: ["cx11"]     # Cheapest option for dev
  image: "ubuntu-22.04"
  locations: ["nbg1"]        # Single location
  ssh_key: "~/.ssh/id_ed25519.pub"
  ssh_private_key_path: "~/.ssh/id_ed25519"
```

### Production Connector

```yaml
# /etc/vpn-rotator/config.yaml
api_url: "https://vpn-rotator.yourdomain.com"
key_path: "/etc/vpn-rotator/client.key"
interface: "wg0"
allowed_ips: "0.0.0.0/0"
dns: "1.1.1.1,8.8.8.8"
poll_interval: 15
retry_attempts: 5
retry_delay: 10
log_level: "info"
log_format: "json"
```

### Split Tunnel Connector

```yaml
# ~/.config/vpn-rotator/config.yaml
api_url: "http://localhost:8080"
key_path: "~/.vpn-rotator/client.key"
interface: "wg0"
# Only route private networks through VPN
allowed_ips: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
dns: "192.168.1.1"  # Use local DNS
poll_interval: 15
log_level: "debug"
log_format: "text"
```

## Security Considerations

### SSH Key Security

1. **Use strong key types**:
   ```bash
   # Recommended: Ed25519
   ssh-keygen -t ed25519 -f ~/.ssh/vpn-rotator
   
   # Alternative: RSA 4096-bit
   ssh-keygen -t rsa -b 4096 -f ~/.ssh/vpn-rotator
   ```

2. **Set proper permissions**:
   ```bash
   chmod 600 ~/.ssh/vpn-rotator      # Private key
   chmod 644 ~/.ssh/vpn-rotator.pub  # Public key
   ```

3. **Use dedicated keys**: Don't reuse SSH keys from other services.

### API Token Security

1. **Use minimal permissions**: Create a Hetzner API token with only required permissions:
   - Servers: Read/Write
   - SSH Keys: Read
   - Networks: Read

2. **Rotate tokens regularly**: Change API tokens every 90 days.

3. **Store securely**: Use environment variables or secure secret management.

### Network Security

1. **Firewall configuration**: Only expose necessary ports:
   ```bash
   # Allow API access
   ufw allow 8080/tcp
   
   # Allow WireGuard
   ufw allow 51820/udp
   ```

2. **TLS termination**: Use a reverse proxy with TLS for production:
   ```nginx
   server {
       listen 443 ssl;
       server_name vpn-rotator.yourdomain.com;
       
       location / {
           proxy_pass http://localhost:8080;
           proxy_set_header Host $host;
           proxy_set_header X-Real-IP $remote_addr;
       }
   }
   ```

### File Permissions

Set appropriate permissions for configuration files:

```bash
# Rotator service config
sudo chmod 600 /etc/vpn-rotator/config.yaml
sudo chown vpn-rotator:vpn-rotator /etc/vpn-rotator/config.yaml

# Connector config
chmod 600 ~/.config/vpn-rotator/config.yaml

# Database directory
sudo mkdir -p /var/lib/vpn-rotator
sudo chmod 700 /var/lib/vpn-rotator
sudo chown vpn-rotator:vpn-rotator /var/lib/vpn-rotator
```

## Troubleshooting

### Common Configuration Issues

#### 1. Invalid Hetzner API Token

**Error**: `failed to initialize Hetzner provisioner: API token is required`

**Solution**:
```bash
# Verify token is set
echo $VPN_ROTATOR_HETZNER_API_TOKEN

# Test token validity
curl -H "Authorization: Bearer $VPN_ROTATOR_HETZNER_API_TOKEN" \
     https://api.hetzner.cloud/v1/servers
```

#### 2. SSH Key Not Found

**Error**: `failed to read SSH public key file`

**Solution**:
```bash
# Check file exists and permissions
ls -la ~/.ssh/vpn-rotator.pub
ls -la ~/.ssh/vpn-rotator

# Verify key format
head -1 ~/.ssh/vpn-rotator.pub
# Should start with: ssh-ed25519 or ssh-rsa
```

#### 3. Database Permission Issues

**Error**: `failed to open database: permission denied`

**Solution**:
```bash
# Create directory with proper permissions
sudo mkdir -p /var/lib/vpn-rotator
sudo chown vpn-rotator:vpn-rotator /var/lib/vpn-rotator
sudo chmod 755 /var/lib/vpn-rotator
```

#### 4. Connector Cannot Reach API

**Error**: `failed to fetch VPN config: connection refused`

**Solution**:
```bash
# Test API connectivity
curl http://your-rotator-service.com:8080/health

# Check firewall
sudo ufw status

# Verify service is running
sudo systemctl status vpn-rotator
```

### Configuration Validation

#### Validate Rotator Service Config

```bash
# Test configuration loading
vpn-rotator --config /path/to/config.yaml --dry-run

# Check API endpoint
curl http://localhost:8080/health
```

#### Validate Connector Config

```bash
# Test configuration loading
vpn-rotator connect --config /path/to/config.yaml --dry-run

# Test API connectivity
vpn-rotator status --api-url http://your-service.com:8080
```

### Debug Mode

Enable debug logging for troubleshooting:

```bash
# Rotator service
export VPN_ROTATOR_LOG_LEVEL=debug
vpn-rotator

# Connector
export VPN_ROTATOR_LOG_LEVEL=debug
sudo -E vpn-rotator connect
```

### Log Analysis

Monitor logs for common issues:

```bash
# Service logs
journalctl -u vpn-rotator -f

# Connector logs (when running as service)
journalctl -u vpn-rotator-connector -f

# Check for specific errors
journalctl -u vpn-rotator | grep -i error
```

For additional help, see:
- [User Guide](user-guide.md) - Basic usage instructions
- [Deployment Guide](deployment.md) - Production deployment
- [Troubleshooting Guide](troubleshooting.md) - Common issues and solutions