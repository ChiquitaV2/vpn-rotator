# VPN Rotator System Requirements v2.0

## Functional Requirements (FR)

What the system must _do_.

### Rotator Service (Backend)

#### Node Lifecycle Management

- **FR-1:** The service MUST run a scheduled job at a configurable interval (default: every 24 hours, configurable via environment variable).
- **FR-2:** The scheduled job MUST check if node rotation is needed based on the following conditions:
    - A node exists and has active clients (last handshake within 3 minutes)
    - The node has been active for longer than the rotation interval
- **FR-3:** When rotation is needed, the service MUST provision a new Hetzner Cloud server using the Hetzner API.
- **FR-4:** The service MUST use a `cloud-init` script to install and configure WireGuard on the newly provisioned server.
- **FR-5:** The service MUST generate a new WireGuard key pair for the server during provisioning.
- **FR-6:** The service MUST perform a health check on the new node by attempting a WireGuard handshake. The node is considered "online" when a successful handshake is established.
- **FR-7:** The service MUST, after confirming the new node is online, mark the old node for destruction with a grace period of 2 hours.
- **FR-8:** The service MUST destroy nodes marked for destruction after the 2-hour grace period expires, unless clients are still connected (see FR-13).
- **FR-9:** The service MUST provision a new node on-demand when a client calls the API and no active node exists.

#### Idle Node Management

- **FR-10:** The service MUST monitor active client connections on all nodes using `wg show` to track last handshake times.
- **FR-11:** The service MUST consider a client "connected" if their last WireGuard handshake occurred within the last 3 minutes.
- **FR-12:** The service MUST destroy idle nodes (nodes with zero connected clients) after 30 minutes of inactivity.
- **FR-13:** The service MUST cancel scheduled node destruction if a client connects or attempts to connect during the grace period.

#### State Management

- **FR-14:** The service MUST persist the following data for each node:
    - Node ID (Hetzner server ID)
    - IP address
    - WireGuard server public key
    - WireGuard port
    - Status (provisioning, active, destroying)
    - Creation timestamp
    - Scheduled destruction timestamp (if applicable)
- **FR-15:** The service MUST restore all persisted state after a service restart.
- **FR-16:** The service MUST handle concurrent state updates safely (no race conditions).

#### API

- **FR-17:** The service MUST expose an HTTP `GET` endpoint `/api/v1/config/latest` that returns the currently active node's connection details.
- **FR-18:** The API response MUST include the following JSON fields:

    ```json
    {  "version": "v1",  "server_public_key": "...",  "server_ip": "...",  "server_port": 51820,  "updated_at": "2025-01-15T10:30:00Z",  "rotation_scheduled": true|false}
    ```

- **FR-19:** The endpoint MUST return HTTP 200 when an active node exists.
- **FR-20:** The endpoint MUST return HTTP 503 when no active node exists and trigger on-demand provisioning (FR-9).
- **FR-21:** The endpoint MUST return HTTP 500 for internal errors with a JSON error message.
- **FR-22:** The endpoint MUST respond within 5 seconds under normal conditions.
- **FR-23:** The endpoint MUST remain read-consistent during node transitions (clients never receive invalid/incomplete config).

#### Resource Cleanup

- **FR-24:** The service MUST track all provisioned Hetzner resources (servers, volumes, etc.).
- **FR-25:** The service MUST implement a cleanup job that runs hourly to identify and destroy orphaned resources (resources not in the database or in "destroying" state for >1 hour).
- **FR-26:** The service MUST log all provisioning, destruction, and cleanup events.

### Connector CLI (Client)

#### Connection Management

- **FR-27:** The CLI MUST have a `connect` command.
- **FR-28:** The `connect` command MUST fetch the active node's details from the Rotator Service's API endpoint.
- **FR-29:** The `connect` command MUST retry failed API requests up to 3 times with exponential backoff (1s, 2s, 4s).
- **FR-30:** The `connect` command MUST generate a WireGuard client key pair on first run and persist it locally (default location: `~/.vpn-rotator/client.key`).
- **FR-31:** The `connect` command MUST reuse the same client key pair across all subsequent connections and rotations.
- **FR-32:** The `connect` command MUST generate a valid WireGuard client `.conf` file locally using:
    - The persisted client private key
    - The server details from the API response
- **FR-33:** The `connect` command MUST validate the downloaded configuration before applying it (valid keys, valid IP, reachable port).
- **FR-34:** The `connect` command MUST execute a local command (e.g., `wg-quick up <interface>`) to establish the VPN connection.
- **FR-35:** The CLI MUST have a `disconnect` command that executes a local command (e.g., `wg-quick down <interface>`) to stop the VPN connection.

#### Auto-Rotation Detection

- **FR-36:** When connected, the CLI MUST poll the Rotator Service API every 15 minutes to check for node rotation.
- **FR-37:** If a new node is detected (different `server_public_key` or `server_ip`), the CLI MUST:
    1. Disconnect from the old node
    2. Generate a new configuration with the new node details
    3. Automatically reconnect to the new node
- **FR-38:** The CLI MUST log all rotation events to stdout/stderr.
- **FR-39:** The CLI MUST continue polling and auto-rotating until the user runs `disconnect` or terminates the process.

---

## Non-Functional Requirements (NFR)

What the system must _be_.

### Technology

- **NFR-1 (Tech Stack):** The Rotator Service and Connector CLI MUST be written in either **Go** or **Kotlin**.
- **NFR-2 (Database):** The Rotator Service MUST use an embedded database (**SQLite**) for state management.
- **NFR-3 (Infrastructure):** The system MUST interact with Hetzner directly via its API and `cloud-init`. It MUST NOT use Terraform or Ansible.

### Scope

- **NFR-4 (Simplicity):** The system MUST NOT include a Web UI, user management, or OIDC authentication for the MVP.
- **NFR-5 (Single User):** The MVP MUST support a single user/operator. Multi-user support is out of scope.

### Operations

- **NFR-6 (Deployment):** The Rotator Service MUST be runnable as a single, persistent process (e.g., a Docker container, systemd service).
- **NFR-7 (Distribution):** The Connector CLI MUST be distributed as a single statically-linked binary for Linux, macOS, and Windows.
- **NFR-8 (Configuration):** All service configuration (Hetzner API token, rotation interval, etc.) MUST be provided via environment variables (prefixed with `VPN_ROTATOR_`), YAML config file, or command-line flags (in that priority order).
- **NFR-9 (Observability):** The Rotator Service MUST emit structured logs in JSON format (configurable to text format for development) with the following levels: DEBUG, INFO, WARN, ERROR. Logs MUST include request IDs for tracing.
- **NFR-10 (Graceful Shutdown):** The Rotator Service MUST handle SIGTERM/SIGINT gracefully:
    - Stop accepting new requests
    - Complete in-flight operations
    - Persist state before exiting
    - Exit within 30 seconds

### Security & Privacy

- **NFR-11 (Secrets Management):** The Hetzner API token MUST be configurable via environment variables (`VPN_ROTATOR_HETZNER_API_TOKEN`) and MUST NOT be hard-coded or logged. The Enhanced Logger MUST redact sensitive fields.
- **NFR-12 (Privacy):** The provisioned WireGuard nodes MUST be configured to not retain client connection logs. Specifically:
    - No WireGuard connection logging
    - No packet capture or traffic logging
    - iptables rules must not log matched packets
- **NFR-13 (Client Keys):** Client private keys MUST never be transmitted over the network or stored on the server.
- **NFR-14 (Request Tracing):** All API requests MUST include a unique `request_id` (UUID v4) for security auditing and debugging.

### Performance

- **NFR-14 (Provisioning Time):** The provisioning of a new node (from API call to passing health check) SHOULD complete in under 5 minutes.
- **NFR-15 (API Latency):** The `/api/v1/config/latest` endpoint MUST respond in under 5 seconds under normal load.
- **NFR-16 (Rotation Downtime):** During node rotation, client connectivity disruption MUST be under 5 seconds (time to detect new node + reconnect).

### Reliability

- **NFR-17 (Idempotency):** All Hetzner API operations MUST be idempotent to handle retry scenarios safely.
- **NFR-18 (Rollback):** If new node provisioning fails after 3 retry attempts, the service MUST:
    - Retain the current active node (if one exists)
    - Log the failure
    - Retry provisioning at the next scheduled interval
- **NFR-19 (Data Durability):** The SQLite database MUST use WAL mode for crash safety.

---

## Configuration Reference

### Rotator Service Environment Variables

All environment variables are prefixed with `VPN_ROTATOR_`.

|Variable|Required|Default|Description|
|---|---|---|---|
|`VPN_ROTATOR_HETZNER_API_TOKEN`|Yes|-|Hetzner Cloud API token|
|`VPN_ROTATOR_ROTATION_INTERVAL_HOURS`|No|24|Hours between scheduled rotations|
|`VPN_ROTATOR_IDLE_TIMEOUT_MINUTES`|No|30|Minutes before destroying idle nodes|
|`VPN_ROTATOR_GRACE_PERIOD_HOURS`|No|2|Hours to wait before destroying old nodes|
|`VPN_ROTATOR_DATABASE_PATH`|No|`./data/rotator.db`|Path to SQLite database|
|`VPN_ROTATOR_API_PORT`|No|8080|HTTP API port|
|`VPN_ROTATOR_LOG_LEVEL`|No|INFO|Logging level (DEBUG, INFO, WARN, ERROR)|
|`VPN_ROTATOR_LOG_FORMAT`|No|text|Log format (text or json)|
|`VPN_ROTATOR_CONFIG_PATH`|No|`config.yaml`|Path to YAML config file|

### Connector CLI Flags

All environment variables are prefixed with `VPN_ROTATOR_`.

|Flag|Environment Variable|Default|Description|
|---|---|---|---|
|`--api-url`|`VPN_ROTATOR_API_URL`|`http://localhost:8080`|Rotator Service API URL|
|`--key-path`|`VPN_ROTATOR_KEY_PATH`|`~/.vpn-rotator/client.key`|Path to client private key|
|`--interface`|`VPN_ROTATOR_INTERFACE`|`wg0`|WireGuard interface name|
|`--poll-interval`|`VPN_ROTATOR_POLL_INTERVAL`|15|Minutes between rotation checks|
|`--config`|`VPN_ROTATOR_CONFIG_PATH`|`config.yaml`|Path to YAML config file|
|`--log-level`|`VPN_ROTATOR_LOG_LEVEL`|INFO|Logging level|
|`--log-format`|`VPN_ROTATOR_LOG_FORMAT`|text|Log format (text or json)|

---

## Success Criteria

The MVP is considered complete when:

1. ✅ A single user can run the Rotator Service and Connector CLI
2. ✅ Nodes are automatically provisioned on-demand
3. ✅ Nodes rotate every 24 hours (configurable)
4. ✅ Idle nodes are destroyed after 30 minutes
5. ✅ Clients auto-reconnect when new nodes are provisioned
6. ✅ Old nodes are destroyed gracefully after 2-hour grace period
7. ✅ System recovers from crashes without data loss
8. ✅ Orphaned Hetzner resources are cleaned up automatically