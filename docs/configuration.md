# Configuration Guide

This document describes the configuration system for the VPN Rotator service and the connector client. The system
supports YAML files, environment variables (via Viper), and sensible hardcoded defaults.

## Key Concepts

- Priority: Environment variables > YAML files > hardcoded defaults.
- Recommended env prefix: `VPN_ROTATOR_`.
- Automatic type conversion: durations (`"24h"`), booleans (`"true"`), numbers, and comma-separated slices (
  `"cx11,cx21"`).

## Search Paths

Viper will search, in order:

- `/etc/vpn-rotator/`
- `$HOME/.vpn-rotator/` (rotator) or `$HOME/.config/vpn-rotator/` (connector)
- Current directory
- Binary directory
- Explicit path via `--config` or loader API

## Configuration structure

1. Essential
    - `hetzner` (api_token, ssh keys, server_types, locations)
    - `rotation` (intervals, cleanup)
    - `peers`(limits, caching)
    - `log` (level, format)

2. Optional
    - `api` (listen address)
    - `database` / `db` (path, tuning)
    - `service` (shutdown timeout)

3. Internal (hardcoded defaults)
    - Network ranges, timeouts, circuit breaker settings (not usually user-configurable)

## File locations

Rotator looks for `config.yaml` at:

- `/etc/vpn-rotator/config.yaml`
- `$HOME/.vpn-rotator/config.yaml`
- `./config.yaml`
- `--config /path/to/config.yaml`

Connector looks for `config.yaml` at:

- `/etc/vpn-rotator/config.yaml`
- `$HOME/.config/vpn-rotator/config.yaml`
- `./config.yaml`
- `--config /path/to/config.yaml`

## Required settings

- Hetzner API token: `hetzner.api_token` or `VPN_ROTATOR_HETZNER_API_TOKEN`
- SSH public and private keys: `hetzner.ssh_key` and `hetzner.ssh_private_key_path`

## Minimal YAML example

```yaml
hetzner:
  api_token: "your-hetzner-token"
  ssh_private_key_path: "/path/to/ssh/key"
  server_types: [ "cx11", "cx21" ]

rotation:
  interval: "24h"
  cleanup_age: "2h"

log:
  level: "info"
  format: "json"

# optional
api:
  listen_addr: ":8080"

database:
  path: "./data/rotator.db"
```

## Minimal environment variables

Preferred, namespaced form:

```bash
export VPN_ROTATOR_HETZNER_API_TOKEN="token"
export VPN_ROTATOR_HETZNER_SSH_PRIVATE_KEY_PATH="/path/to/ssh/key"
export VPN_ROTATOR_ROTATION_INTERVAL="12h"
export VPN_ROTATOR_LOG_LEVEL="debug"
```

## Security

- Use dedicated SSH keys (Ed25519 recommended) and set strict permissions (`600` for private keys).
- Store Hetzner token securely and rotate regularly.
- Restrict API exposure (use reverse proxy/TLS) and firewall necessary ports only (e.g., `8080`, WireGuard `51820/udp`).

## Troubleshooting

- Hetzner token missing: check `VPN_ROTATOR_HETZNER_API_TOKEN`.
- SSH key errors: verify paths and permissions.
- DB permission: check file ownership and directory perms.
- Connector connectivity: test `GET /api/v1/health`.
