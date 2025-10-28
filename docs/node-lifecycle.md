# VPN Node Lifecycle Documentation

This document explains how VPN nodes are deployed, used, and destroyed in the VPN Rotator service.

## Overview

The VPN Rotator service manages WireGuard VPN nodes on Hetzner Cloud infrastructure. It automatically provisions new nodes, rotates them periodically for security, and cleans up old nodes to maintain a single active VPN endpoint.

## Architecture Components

- **NodeManager**: Handles direct node operations (create, destroy, health checks)
- **Orchestrator**: Coordinates node lifecycle and rotation logic
- **Provisioner**: Interfaces with Hetzner Cloud API for server management
- **Scheduler**: Manages periodic rotation and cleanup tasks
- **Database**: Tracks node state and metadata

## Node Lifecycle States

```mermaid
stateDiagram-v2
    [*] --> Provisioning : Create Request
    Provisioning --> HealthCheck : Server Created
    HealthCheck --> Active : Health Check Passed
    HealthCheck --> Failed : Health Check Failed
    Active --> ScheduledDestruction : Rotation Triggered
    ScheduledDestruction --> Destroying : Grace Period Expired
    Destroying --> [*] : Node Destroyed
    Failed --> Destroying : Cleanup Failed Node
    Destroying --> [*] : Cleanup Complete
```

## Complete Node Deployment Flow

```mermaid
sequenceDiagram
    participant Client as VPN Client
    participant API as API Server
    participant Orch as Orchestrator
    participant NM as NodeManager
    participant Prov as Hetzner Provisioner
    participant DB as Database
    participant Cloud as Hetzner Cloud

    Note over Client,Cloud: Initial Node Creation
    Client->>API: GET /api/v1/config/latest
    API->>Orch: GetLatestConfig()
    Orch->>DB: GetActiveNode()
    DB-->>Orch: No active node found
    
    Orch->>NM: CreateNode()
    NM->>Prov: ProvisionNode()
    
    Note over Prov,Cloud: Provisioning Phase
    Prov->>Prov: Generate WireGuard Keys
    Prov->>Prov: Render Cloud-Init Template
    Prov->>Cloud: Create Server with Cloud-Init
    Cloud-->>Prov: Server Created (ID, IP)
    Prov->>Prov: Wait 60s for Cloud-Init
    
    Note over Prov,Cloud: Health Check Phase
    loop Health Check Retries (6 attempts)
        Prov->>Cloud: HTTP GET http://IP:8080/health
        Cloud-->>Prov: Health Status
    end
    
    Prov-->>NM: Node Ready (ID, IP, PublicKey)
    
    Note over NM,DB: Database Storage Phase
    NM->>DB: CreateNode(ID, IP, PublicKey, Status="provisioning")
    DB-->>NM: Node Stored
    NM->>DB: MarkNodeActive(ID)
    DB-->>NM: Node Marked Active
    
    NM-->>Orch: NodeConfig(IP, PublicKey)
    Orch-->>API: NodeConfig
    API-->>Client: VPN Configuration
    
    Note over Client: Client connects to VPN
```

## Node Rotation Process

```mermaid
sequenceDiagram
    participant Sched as Scheduler
    participant Orch as Orchestrator
    participant NM as NodeManager
    participant DB as Database
    participant Cloud as Hetzner Cloud

    Note over Sched,Cloud: Periodic Rotation (24h interval)
    Sched->>Orch: RotateNodes()
    Orch->>DB: GetActiveNode()
    DB-->>Orch: Current Active Node
    
    Note over Orch,Cloud: Provision Replacement Node
    Orch->>NM: CreateNode()
    NM->>Cloud: Provision New Server
    Cloud-->>NM: New Node Ready
    NM->>DB: Store & Activate New Node
    
    Note over Orch,DB: Schedule Old Node Destruction
    Orch->>DB: ScheduleNodeDestruction(oldNode, destroyAt=now+1h)
    DB-->>Orch: Old Node Scheduled
    
    Note over Orch: Grace Period (1 hour)
    Note over Orch: Allows existing clients to reconnect
```

## Node Cleanup Process

```mermaid
sequenceDiagram
    participant Sched as Cleanup Scheduler
    participant Orch as Orchestrator
    participant NM as NodeManager
    participant DB as Database
    participant Cloud as Hetzner Cloud

    Note over Sched,Cloud: Periodic Cleanup (1h interval)
    Sched->>Orch: GetOrphanedNodes(age=2h)
    Orch->>DB: GetNodesForDestruction()
    DB-->>Orch: Nodes past destroy time
    
    loop For each orphaned node
        Orch->>NM: DestroyNode(node)
        NM->>Cloud: Delete Server
        Cloud-->>NM: Server Deleted
        NM->>DB: DeleteNode(nodeID)
        DB-->>NM: Node Removed
    end
```

## Service Shutdown Cleanup

```mermaid
sequenceDiagram
    participant Service as Service
    participant Orch as Orchestrator
    participant NM as NodeManager
    participant DB as Database
    participant Cloud as Hetzner Cloud

    Note over Service,Cloud: Graceful Shutdown
    Service->>Service: Stop Signal Received
    Service->>Orch: GetNodesByStatus("active")
    Orch->>DB: Query Active Nodes
    DB-->>Orch: List of Active Nodes
    
    loop For each active node
        Service->>Orch: DestroyNode(node)
        Orch->>NM: DestroyNode(node)
        NM->>Cloud: Delete Server
        Cloud-->>NM: Server Deleted
        NM->>DB: DeleteNode(nodeID)
        DB-->>NM: Node Removed
    end
    
    Service->>Service: Shutdown Complete
```

## Cloud-Init Configuration

When a new server is created, it's configured with a cloud-init script that:

1. **Installs WireGuard** and required packages
2. **Configures WireGuard interface** with generated keys
3. **Sets up firewall rules** (UFW) for VPN traffic
4. **Enables IP forwarding** for routing
5. **Starts health check server** on port 8080
6. **Configures systemd services** for auto-start

### Health Check Endpoint

Each node runs a Python health server that:
- Listens on port 8080
- Provides `/health` endpoint
- Checks if WireGuard interface is running
- Returns JSON status response

```json
{
  "status": "healthy",
  "timestamp": "2025-10-27T21:30:00.000Z",
  "service": "wireguard"
}
```

## Error Handling and Recovery

```mermaid
flowchart TD
    A[Node Creation Request] --> B{Provisioner Success?}
    B -->|No| C[Return Error]
    B -->|Yes| D{Database Storage Success?}
    D -->|No| E[Cleanup Provisioned Node]
    E --> C
    D -->|Yes| F{Health Check Success?}
    F -->|No| G[Cleanup Node & Database]
    G --> C
    F -->|Yes| H{Mark Active Success?}
    H -->|No| I[Cleanup Node & Database]
    I --> C
    H -->|Yes| J[Node Ready for Use]
    
    style C fill:#ffcccc
    style J fill:#ccffcc
```

## Key Features

### 1. **Atomic Operations**
- Node creation is atomic - if any step fails, everything is cleaned up
- Database transactions ensure consistency
- No orphaned resources left behind

### 2. **Health Verification**
- Every node is health-checked before being marked active
- Health checks verify WireGuard is actually running
- Failed nodes are automatically cleaned up

### 3. **Graceful Rotation**
- New node is created before old node is destroyed
- Grace period allows existing clients to reconnect
- Zero-downtime rotation process

### 4. **Resource Cleanup**
- Automatic cleanup of expired nodes
- Service shutdown cleans up all active nodes
- No cloud resources left running after service stops

### 5. **Error Recovery**
- Failed provisioning attempts are cleaned up
- Unhealthy nodes are automatically destroyed
- Robust error handling at every step

## Configuration

Key configuration parameters:

- **Rotation Interval**: 24 hours (configurable)
- **Cleanup Interval**: 1 hour (configurable)
- **Grace Period**: 1 hour before node destruction
- **Health Check Timeout**: 10 seconds per attempt
- **Health Check Retries**: 6 attempts with 10s intervals
- **Cloud-Init Wait Time**: 60 seconds

## Monitoring and Observability

The service provides comprehensive logging for:
- Node provisioning events
- Health check results
- Rotation and cleanup operations
- Error conditions and recovery actions
- Database operations and state changes

All operations include structured logging with node IDs, IP addresses, and timestamps for easy debugging and monitoring.