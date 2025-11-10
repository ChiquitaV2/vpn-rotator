# API Reference

This document provides a detailed reference for the VPN Rotator Service API.

## Base URL

The base URL for the API is the address of your VPN Rotator service. For example, `http://localhost:8080`.

## Authentication

The API is open and does not require authentication. It is expected to be run in a trusted environment.

## Endpoints

### `POST /api/v1/connect`

Connects a new peer to the VPN. If no active node is available, it triggers on-demand provisioning. The request body
should contain the peer's public key.

**Request:**

```json
{
  "public_key": "...",
  "generate_keys": false
}
```

**Response (200 OK):**

```json
{
  "peer_id": "...",
  "server_config": {
    "server_public_key": "...",
    "server_ip": "...",
    "server_port": 51820
  },
  "client_ip": "10.8.1.2"
}
```

**Response (202 Accepted):**

Returned when no active node is available and provisioning is triggered.

```json
{
  "error": "provisioning_required",
  "message": "No active node available. Provisioning has been initiated.",
  "estimated_wait": "5m0s",
  "retry_after": 300
}
```

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