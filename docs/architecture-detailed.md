# Architecture

This document describes the system architecture of VPN Rotator with detailed diagrams.

## System Overview

VPN Rotator is a self-hosted system that automatically rotates WireGuard VPN nodes on Hetzner Cloud to enhance privacy and security. It provisions nodes on-demand, automatically destroys idle nodes to save costs, and handles seamless client migration during rotation.

### Key Components

1. **Rotator Service** - Core backend service managing node lifecycle and peer management
2. **Connector Client** - CLI tool for client devices
3. **Database** - SQLite for state persistence with peer and subnet management
4. **Hetzner Cloud** - Infrastructure provider for WireGuard nodes
5. **Peer Management System** - Comprehensive peer lifecycle management with IP allocation

## High-Level Architecture

```mermaid
graph TB
    subgraph "Client Devices"
        A["Laptop<br/>WireGuard CLI"]
        B["Phone<br/>WireGuard App"]
        C["Tablet<br/>WireGuard App"]
    end
    
    subgraph "VPN Rotator System"
        D["Rotator Service<br/>API & Orchestrator"]
        E["SQLite DB<br/>State Persistence"]
        F["Connector CLI<br/>Client Application"]
    end
    
    subgraph "Hetzner Cloud"
        G["Active Node<br/>wg-node-001"]
        H["Old Node<br/>wg-node-002<br/>(Grace Period)"]
        I["New Node<br/>wg-node-003<br/>(Provisioning)"]
    end
    
    A <-->|WireGuard UDP| G
    B <-->|WireGuard UDP| G
    C <-->|WireGuard UDP| G
    
    D <-->|API Calls| G
    D <-->|API Calls| H
    D <-->|API Calls| I
    D <--> E
    
    F <-->|HTTP API| D
    A <-->|Auto-Rotation| F
    B <-->|Auto-Rotation| F
    C <-->|Auto-Rotation| F
    
    style D fill:#4CAF50,stroke:#388E3C,color:#fff
    style G fill:#2196F3,stroke:#0D47A1,color:#fff
    style E fill:#9C27B0,stroke:#7B1FA2,color:#fff
```

## Component Architecture

```mermaid
graph LR
    subgraph "Rotator Service"
        A["HTTP API<br/>Peer Management Endpoints"]
        B["Scheduler<br/>Rotation & Cleanup"]
        C["Provisioner<br/>Hetzner API"]
        D["Orchestrator<br/>Peer-Aware State Management"]
        E["Health Checker<br/>WireGuard Validation"]
        F["Config Loader<br/>YAML + ENV"]
        G["Enhanced Logger<br/>Structured Logging"]
        H["Peer Manager<br/>Lifecycle & Database Ops"]
        I["IP Manager<br/>Subnet & IP Allocation"]
        J["Node Manager<br/>SSH & Peer Config"]
    end
    
    subgraph "Data Layer"
        K["SQLite DB<br/>Nodes, Peers, Subnets"]
    end
    
    subgraph "External Services"
        L["Hetzner Cloud<br/>API & VMs"]
        M["Cloud Init<br/>Scripts"]
        N["SSH Pool<br/>Node Connections"]
    end
    
    A --> D
    A --> H
    A --> I
    A --> J
    B --> D
    D --> C
    D --> H
    D --> I
    D --> J
    D --> E
    H <--> K
    I <--> K
    J --> I
    J --> N
    D <--> K
    C --> L
    C --> M
    F --> A
    F --> D
    G --> A
    G --> D
    
    style A fill:#FFC107,stroke:#FF8F00,color:#000
    style C fill:#2196F3,stroke:#0D47A1,color:#fff
    style K fill:#9C27B0,stroke:#7B1FA2,color:#fff
    style G fill:#4CAF50,stroke:#388E3C,color:#fff
    style H fill:#E91E63,stroke:#C2185B,color:#fff
    style I fill:#00BCD4,stroke:#0097A7,color:#fff
    style J fill:#FF5722,stroke:#D84315,color:#fff


```

## Peer Management Architecture

```mermaid
graph TB
    subgraph "API Layer"
        A1["POST /api/v1/connect<br/>Peer Connection"]
        A2["DELETE /api/v1/disconnect<br/>Peer Removal"]
        A3["GET /api/v1/peers<br/>Peer Listing"]
        A4["GET /api/v1/peers/{id}<br/>Peer Details"]
    end
    
    subgraph "Core Components"
        B1["Orchestrator<br/>Node Selection & Load Balancing"]
        B2["Peer Manager<br/>Database Operations & Validation"]
        B3["IP Manager<br/>Subnet & IP Allocation"]
        B4["Node Manager<br/>SSH Operations & WireGuard Config"]
    end
    
    subgraph "Data Layer"
        C1["peers table<br/>Peer configurations"]
        C2["node_subnets table<br/>IP allocation tracking"]
        C3["nodes table<br/>Node information"]
    end
    
    subgraph "Infrastructure"
        D1["SSH Pool<br/>Connection Management"]
        D2["WireGuard Nodes<br/>Peer Configuration"]
        D3["Circuit Breakers<br/>Resilience"]
    end
    
    A1 --> B1
    A1 --> B2
    A1 --> B3
    A1 --> B4
    A2 --> B2
    A2 --> B4
    A3 --> B2
    A4 --> B2
    
    B1 --> C3
    B2 --> C1
    B3 --> C2
    B4 --> D1
    B4 --> D2
    
    B1 --> B2
    B1 --> B3
    B2 --> B3
    B4 --> B3
    
    D3 --> B2
    D3 --> B3
    D3 --> B4
    
    style A1 fill:#4CAF50,stroke:#388E3C,color:#fff
    style A2 fill:#F44336,stroke:#D32F2F,color:#fff
    style B1 fill:#2196F3,stroke:#0D47A1,color:#fff
    style B2 fill:#E91E63,stroke:#C2185B,color:#fff
    style B3 fill:#00BCD4,stroke:#0097A7,color:#fff
    style B4 fill:#FF5722,stroke:#D84315,color:#fff
```

## Peer Connection Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant API as API Server
    participant O as Orchestrator
    participant PM as Peer Manager
    participant IM as IP Manager
    participant NM as Node Manager
    participant DB as Database
    participant Node as VPN Node
    
    Note over C,API: Peer Connection Request
    C->>API: POST /api/v1/connect {public_key}
    API->>API: Validate request & generate request ID
    API->>O: SelectNodeForPeer()
    O->>DB: Get active nodes & peer counts
    O->>IM: GetAvailableIPCount(nodeID)
    O-->>API: Selected nodeID
    
    API->>IM: AllocateClientIP(nodeID)
    IM->>DB: Get node subnet & allocated IPs
    IM->>IM: Find next available IP
    IM-->>API: Allocated IP
    
    API->>PM: CreatePeer(nodeID, publicKey, IP)
    PM->>DB: Insert peer record
    PM-->>API: Peer configuration
    
    API->>O: GetNodeConfig(nodeID)
    O->>DB: Get node details
    O-->>API: Node IP address
    
    API->>NM: AddPeerToNode(nodeIP, peerConfig)
    NM->>Node: SSH: wg set wg0 peer {publicKey} allowed-ips {IP}
    Node-->>NM: Success
    NM-->>API: Peer added
    
    API-->>C: {peer_id, server_config, client_ip}
    
    Note over C,Node: Rollback on failure
    alt SSH Configuration Fails
        API->>PM: RemovePeer(peerID)
        API->>IM: ReleaseClientIP(nodeID, IP)
        API-->>C: Error response
    end
```

## IP Allocation Architecture

```mermaid
graph TB
    subgraph "IP Management"
        A["Base Network<br/>10.8.0.0/16"]
        B["Node Subnets<br/>/24 per node"]
        C["IP Allocation<br/>Per peer"]
    end
    
    subgraph "Node 1 (10.8.1.0/24)"
        D1["Gateway: 10.8.1.1"]
        D2["Range: 10.8.1.2-254"]
        D3["Peer 1: 10.8.1.2"]
        D4["Peer 2: 10.8.1.3"]
        D5["Available: 10.8.1.4-254"]
    end
    
    subgraph "Node 2 (10.8.2.0/24)"
        E1["Gateway: 10.8.2.1"]
        E2["Range: 10.8.2.2-254"]
        E3["Peer 1: 10.8.2.2"]
        E4["Available: 10.8.2.3-254"]
    end
    
    subgraph "Database Tracking"
        F1["node_subnets table<br/>Subnet allocations"]
        F2["peers table<br/>IP assignments"]
    end
    
    A --> B
    B --> D1
    B --> E1
    D2 --> D3
    D2 --> D4
    D2 --> D5
    E2 --> E3
    E2 --> E4
    
    D1 --> F1
    E1 --> F1
    D3 --> F2
    D4 --> F2
    E3 --> F2
    
    style A fill:#4CAF50,stroke:#388E3C,color:#fff
    style B fill:#2196F3,stroke:#0D47A1,color:#fff
    style F1 fill:#9C27B0,stroke:#7B1FA2,color:#fff
    style F2 fill:#FF5722,stroke:#D84315,color:#fff
```

## Node Lifecycle Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant API as API Layer
    participant O as Orchestrator
    participant D as Database
    participant P as Provisioner
    participant H as Hetzner Cloud
    
    Note over C,API: Initial Connection
    C->>API: GET /api/v1/config/latest
    API->>API: Generate Request ID
    API->>API: Log Request
    API->>O: GetLatestConfig()
    O->>D: Check for active nodes
    alt No active nodes
        O->>P: ProvisionNode()
        P->>H: Create new server (wg-node-001)
        H-->>P: Server ID, IP
        P->>H: Configure via cloud-init
        P->>P: Wait for health check
        P-->>O: Node provisioned
        O->>D: Store node state (active)
        O-->>API: Return node config
        API->>API: WriteSuccess()
        API-->>C: {success: true, data: {...}, request_id}
    else Active node exists
        O->>D: Get active node config
        O-->>API: Return existing config
        API->>API: WriteSuccess()
        API-->>C: {success: true, data: {...}, request_id}
    end
    
    Note over C,API: Rotation Time
    O->>P: ProvisionNode()
    P->>H: Create new server (wg-node-002)
    H-->>P: Server ID, IP
    P->>H: Configure via cloud-init
    P->>P: Wait for health check
    P-->>O: Node provisioned
    O->>D: Mark wg-node-001 for destruction
    O->>D: Update active node to wg-node-002
    
    Note over C,API: Client Auto-Rotation
    C->>API: Poll API (every 15 min)
    API->>O: GetLatestConfig()
    O->>D: Get active node
    O-->>API: New config (wg-node-002)
    API-->>C: {success: true, data: {...}, request_id}
    C->>C: Disconnect from wg-node-001
    C->>C: Connect to wg-node-002
    
    Note over O,H: Grace Period Ends
    O->>P: DestroyNode(wg-node-001)
    P->>H: Delete server
    O->>D: Remove node record
```

## Data Flow Architecture

```mermaid
graph LR
    A["Client Device"] -->|1. Connect Request| B["Rotator API"]
    B -->|2. DB Query| C["SQLite DB"]
    C -->|3. Node Status| B
    B -->|4a. No Node| D["Hetzner API"]
    B -->|4b. Active Node| E["Return Config"]
    D -->|5. Provision Server| F["Cloud Init"]
    F -->|6. Install WireGuard| G["VPN Node"]
    G -->|7. Health Check| B
    B -->|8. Store Config| C
    B -->|9. Return Config| E
    E -->|10. WireGuard Config| A
    A -->|11. VPN Traffic| G
    
    style B fill:#4CAF50,stroke:#388E3C,color:#fff
    style C fill:#9C27B0,stroke:#7B1FA2,color:#fff
    style G fill:#2196F3,stroke:#0D47A1,color:#fff
```

## Node Rotation Process

```mermaid
graph TD
    A[Rotation Timer<br/>Scheduler] --> B{Rotation Needed?}
    B -->|No| A
    B -->|Yes| C[Orchestrator:<br/>Provision New Node]
    C --> D[Provisioner:<br/>Create Hetzner Server]
    D --> E[Wait for Health Check]
    E --> F{Health Check OK?}
    F -->|No| G[Retry/Mark Failed]
    G --> H[Log Error]
    F -->|Yes| I[Orchestrator:<br/>Mark Old Node for Destruction]
    I --> J[Update Active Node in DB]
    J --> K[Begin Grace Period]
    K --> L{Grace Period Elapsed?}
    L -->|No| M{Any Clients Connected?}
    M -->|Yes| N[Extend Grace Period]
    N --> K
    M -->|No| O[Provisioner:<br/>Destroy Old Node]
    L -->|Yes| O
    O --> P[Clean Up DB Records]
    P --> A
    
    style C fill:#FFC107,stroke:#FF8F00,color:#000
    style J fill:#4CAF50,stroke:#388E3C,color:#fff
    style O fill:#F44336,stroke:#D32F2F,color:#fff
    style H fill:#FF5722,stroke:#D84315,color:#fff
```

## Client Auto-Rotation Flow

```mermaid
sequenceDiagram
    participant Cl as Connector CLI
    participant API as API Layer
    participant Nd1 as Old Node
    participant Nd2 as New Node
    
    Cl->>API: Connect to VPN
    API-->>Cl: {success: true, data: {config...}}
    Cl->>Nd1: Establish WireGuard connection
    
    loop Auto-Rotation Polling (every 15 min)
        Cl->>API: GET /api/v1/config/latest
        API->>API: Log request with Request ID
        API-->>Cl: {success: true, data: {...}, request_id}
        alt Config Changed (new node)
            Cl->>Cl: Log: "new node detected"
            Cl->>Nd1: Disconnect (wg-quick down)
            Cl->>Cl: Generate new config
            Cl->>Nd2: Connect (wg-quick up)
            Cl->>Cl: Log: "successfully migrated"
        end
    end
    
    Note over API,Nd2: New node becomes active
    Note over Nd1: Old node in grace period
```

## Security Architecture

```mermaid
graph TB
    subgraph "Trust Boundaries"
        A["Client Device"]
        B["VPN Rotator<br/>Service Server"]
        C["Hetzner Cloud<br/>VPN Nodes"]
    end
    
    subgraph "A: Client Security"
        A1["Client Keys<br/>(~/.vpn-rotator/)"]
        A2["Local WireGuard<br/>Config"]
        A3["Config Loader<br/>YAML + ENV"]
    end
    
    subgraph "B: Service Security"
        B1["API Endpoint<br/>HTTPS + Middleware"]
        B2["Request ID<br/>Tracing"]
        B3["SQLite DB<br/>Node State"]
        B4["Hetzner API<br/>Token (ENV)"]
    end
    
    subgraph "C: Node Security"
        C1["WireGuard<br/>Encryption"]
        C2["No Logs<br/>Policy"]
        C3["Auto-Destroy<br/>(Grace Period)"]
    end
    
    A1 -.-> A2
    A3 -.-> A1
    B1 -.-> B2
    B1 -.-> B3
    B1 -.-> B4
    C1 -.-> C2
    C2 -.-> C3
    
    A2 --> B1
    B3 --> C1
    B4 --> C1
    
    style A fill:#E8F5E8,stroke:#4CAF50,color:#000
    style B fill:#E3F2FD,stroke:#2196F3,color:#000
    style C fill:#FFF3E0,stroke:#FF9800,color:#000
```

## API Request Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant M1 as RequestID<br/>Middleware
    participant M2 as Logging<br/>Middleware
    participant M3 as CORS<br/>Middleware
    participant M4 as Recovery<br/>Middleware
    participant H as Handler
    participant O as Orchestrator
    
    C->>M1: HTTP Request
    M1->>M1: Generate UUID
    M1->>M2: Add X-Request-ID header
    M2->>M2: Log request start
    M2->>M3: Continue
    M3->>M3: Add CORS headers
    M3->>M4: Continue
    M4->>H: Execute handler
    H->>O: Business logic
    O-->>H: Result
    H->>H: WriteSuccess() or WriteError()
    H-->>M4: Response
    M4-->>M2: Continue
    M2->>M2: Log request complete
    M2-->>C: {success, data, request_id}
    
    Note over C,O: All responses include request_id<br/>for tracing and debugging
```

## Deployment Architecture

```mermaid
graph LR
    subgraph "User Infrastructure"
        A["User Server<br/>(Rotator Service)"]
        subgraph "Container Runtime"
            B["Rotator Service<br/>Container"]
            C["Database<br/>Volume"]
            D["Config<br/>Volume"]
        end
    end
    
    subgraph "External Services"
        E["Hetzner Cloud<br/>API"]
        F["Hetzner Servers<br/>(VPN Nodes)"]
        G["Client Devices<br/>(Connector CLI)"]
    end
    
    A --> B
    A --> C
    A --> D
    B --> E
    B --> F
    G --> B
    G --> F
    
    style A fill:#4CAF50,stroke:#388E3C,color:#fff
    style B fill:#2196F3,stroke:#0D47A1,color:#fff
    style F fill:#FFC107,stroke:#FF8F00,color:#000
```

## Configuration Architecture

```mermaid
graph TD
    subgraph "Configuration Sources (Priority Order)"
        A["1. Command-line Flags<br/>(Highest)"]
        B["2. Environment Variables<br/>(VPN_ROTATOR_*)"]
        C["3. Config File<br/>(config.yaml)"]
        D["4. Defaults<br/>(Lowest)"]
    end
    
    subgraph "Config Loader"
        E["Viper<br/>Configuration Manager"]
        F["Validation"]
        G["Path Expansion"]
    end
    
    subgraph "Application"
        H["Config Struct"]
        I["Service Components"]
    end
    
    A --> E
    B --> E
    C --> E
    D --> E
    E --> F
    F --> G
    G --> H
    H --> I
    
    style E fill:#4CAF50,stroke:#388E3C,color:#fff
    style F fill:#FFC107,stroke:#FF8F00,color:#000
    style H fill:#2196F3,stroke:#0D47A1,color:#fff
```

## Logging Architecture

```mermaid
graph LR
    subgraph "Application Components"
        A["API Handlers"]
        B["Orchestrator"]
        C["Provisioner"]
        D["Scheduler"]
    end
    
    subgraph "Enhanced Logger"
        E["Logger Instance"]
        F["Request Context"]
        G["Structured Fields"]
    end
    
    subgraph "Output Formats"
        H["Text Format<br/>(Development)"]
        I["JSON Format<br/>(Production)"]
    end
    
    subgraph "Log Levels"
        J["Debug"]
        K["Info"]
        L["Warn"]
        M["Error"]
    end
    
    A --> E
    B --> E
    C --> E
    D --> E
    E --> F
    E --> G
    F --> H
    F --> I
    G --> H
    G --> I
    
    style E fill:#4CAF50,stroke:#388E3C,color:#fff
    style I fill:#2196F3,stroke:#0D47A1,color:#fff
```

## Error Handling Flow

```mermaid
graph TD
    A[Error Occurs] --> B{Error Type?}
    B -->|Validation| C[ValidationError]
    B -->|Not Found| D[NotFoundError]
    B -->|API Error| E[APIError]
    B -->|Other| F[Generic Error]
    
    C --> G[Log Error with Context]
    D --> G
    E --> G
    F --> G
    
    G --> H[Enrich with Request ID]
    H --> I[Map to HTTP Status]
    I --> J[WriteError Response]
    
    J --> K{Success: false<br/>Error: code, message, request_id}
    
    style G fill:#FFC107,stroke:#FF8F00,color:#000
    style J fill:#F44336,stroke:#D32F2F,color:#fff
    style K fill:#2196F3,stroke:#0D47A1,color:#fff
```