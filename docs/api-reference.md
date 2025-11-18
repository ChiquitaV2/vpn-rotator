# API Reference

This document provides a detailed reference for the VPN Rotator Service API.

## Base URL

The base URL for the API is the address of your VPN Rotator service. For example, `http://localhost:8080`.

## Authentication

The API is open and does not require authentication. It is expected to be run in a trusted environment.

## Endpoints

### `POST /api/v1/connect`

Initiates an asynchronous peer connection to the VPN. Returns immediately with a request ID that can be used to poll for
connection status. If no active node is available, provisioning is triggered automatically.

**Request:**

```json
{
  "public_key": "...",
  "generate_keys": false
}
```

**Response (202 Accepted):**

The connection request has been accepted and is being processed asynchronously.

```json
{
  "success": true,
  "data": {
    "request_id": "req_abc123xyz",
    "message": "Connection request accepted and is being processed",
    "status_endpoint": "/api/v1/connections/req_abc123xyz"
  }
}
```

**Error Response (400 Bad Request):**

```json
{
  "success": false,
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid public key format",
    "request_id": "req_xyz789"
  }
}
```

### `GET /api/v1/connections/{requestID}`

Retrieves the status of an asynchronous peer connection request. Use this endpoint to poll for the completion of a
connection initiated via `POST /api/v1/connect`.

**Response (200 OK) - In Progress:**

```json
{
  "success": true,
  "data": {
    "request_id": "req_abc123xyz",
    "peer_id": "",
    "phase": "provisioning_node",
    "progress": 45.0,
    "is_active": true,
    "message": "Provisioning WireGuard node...",
    "started_at": "2025-11-18T10:00:00Z",
    "last_updated": "2025-11-18T10:02:30Z",
    "estimated_eta": "2025-11-18T10:05:00Z"
  }
}
```

**Response (200 OK) - Completed:**

```json
{
  "success": true,
  "data": {
    "request_id": "req_abc123xyz",
    "peer_id": "peer_123",
    "phase": "completed",
    "progress": 100.0,
    "is_active": false,
    "message": "Peer connected successfully",
    "started_at": "2025-11-18T10:00:00Z",
    "last_updated": "2025-11-18T10:05:00Z",
    "connection_details": {
      "peer_id": "peer_123",
      "server_public_key": "...",
      "server_ip": "...",
      "server_port": 51820,
      "client_ip": "10.8.1.2",
      "dns": ["8.8.8.8"],
      "allowed_ips": ["0.0.0.0/0"]
    }
  }
}
```

**Response (200 OK) - Failed:**

```json
{
  "success": true,
  "data": {
    "request_id": "req_abc123xyz",
    "phase": "failed",
    "progress": 0.0,
    "is_active": false,
    "message": "Connection failed",
    "error_message": "Failed to provision node: timeout",
    "started_at": "2025-11-18T10:00:00Z",
    "last_updated": "2025-11-18T10:05:00Z"
  }
}
```

**Response (404 Not Found):**

```json
{
  "success": false,
  "error": {
    "code": "NOT_FOUND",
    "message": "Connection request not found",
    "request_id": "req_unknown"
  }
}
```

**Progress Phases:**

- `initializing` (0%) - Connection request received
- `validating_request` (10%) - Validating request parameters
- `selecting_node` (20%) - Selecting or provisioning a node
- `provisioning_node` (40%) - Provisioning new node (if needed)
- `configuring_peer` (60%) - Configuring WireGuard peer
- `activating_connection` (80%) - Activating connection
- `completed` (100%) - Connection successful
- `failed` (0%) - Connection failed

### `DELETE /api/v1/disconnect`

Disconnects a peer from the VPN. The request body should contain the peer's ID.

**Request:**

```json
{
  "peer_id": "..."
}
```

**Response (200 OK):**

```json
{
  "message": "Peer disconnected successfully",
  "peer_id": "..."
}
```

### `GET /health`

Returns the health status of the rotator service.

**Response (200 OK):**

```json
{
  "status": "healthy",
  "version": "1.0.0"
}
```

### `GET /api/v1/peers`

Lists all active peers.

**Response (200 OK):**

```json
{
  "peers": [
    {
      "id": "...",
      "node_id": "...",
      "public_key": "...",
      "client_ip": "10.8.1.2",
      "connected_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total_count": 1,
  "offset": 0,
  "limit": 10
}
```

### `GET /api/v1/peers/{peerID}`

Gets the status of a specific peer.

**Response (200 OK):**

```json
{
  "id": "...",
  "node_id": "...",
  "public_key": "...",
  "client_ip": "10.8.1.2",
  "connected_at": "2025-01-15T10:30:00Z",
  "last_handshake": "2025-01-15T10:35:00Z",
  "rx_bytes": 1234,
  "tx_bytes": 5678
}
```

## Admin Endpoints

### `GET /api/v1/admin/status`

Gets the overall status of the system.

### `GET /api/v1/admin/stats/nodes`

Gets statistics about the nodes.

### `GET /api/v1/admin/stats/peers`

Gets statistics about the peers.

### `POST /api/v1/admin/rotations`

Forces a node rotation.

### `GET /api/v1/admin/rotations/status`

Gets the status of the current rotation.

### `POST /api/v1/admin/cleanup`

Triggers a manual cleanup of resources.

### `GET /api/v1/admin/health`

Performs a detailed health check of the system.

### `GET /api/v1/provisioning/status`

Gets the status of the current provisioning process.