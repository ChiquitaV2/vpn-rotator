# Architecture

This document describes the system architecture of VPN Rotator with detailed diagrams.

## System Overview

VPN Rotator is a self-hosted system that automatically rotates WireGuard VPN nodes on Hetzner Cloud to enhance privacy and security. It provisions nodes on-demand, automatically destroys idle nodes to save costs, and handles seamless client migration during rotation.

### Key Components

1. **Rotator Service** - Core backend service managing node lifecycle
2. **Connector Client** - CLI tool for client devices
3. **Database** - SQLite for state persistence
4. **Hetzner Cloud** - Infrastructure provider for WireGuard nodes

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
        A["HTTP API<br/>Handlers + Middleware"]
        B["Scheduler<br/>Rotation & Cleanup"]
        C["Provisioner<br/>Hetzner API"]
        D["Orchestrator<br/>State Management"]
        E["Health Checker<br/>WireGuard Validation"]
        F["Config Loader<br/>YAML + ENV"]
        G["Enhanced Logger<br/>Structured Logging"]
    end
    
    subgraph "Data Layer"
        H["SQLite DB<br/>with Migrations"]
    end
    
    subgraph "External Services"
        I["Hetzner Cloud<br/>API & VMs"]
        J["Cloud Init<br/>Scripts"]
    end
    
    A --> D
    B --> D
    D --> C
    D --> E
    D <--> H
    C --> I
    C --> J
    F --> A
    F --> D
    G --> A
    G --> D
    
    style A fill:#FFC107,stroke:#FF8F00,color:#000
    style C fill:#2196F3,stroke:#0D47A1,color:#fff
    style H fill:#9C27B0,stroke:#7B1FA2,color:#fff
    style G fill:#4CAF50,stroke:#388E3C,color:#fff
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