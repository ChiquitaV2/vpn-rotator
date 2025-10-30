-- Node status values: provisioning, active, destroying
-- Status transitions: provisioning -> active -> destroying -> (deleted)
CREATE TABLE nodes
(
    -- Identity
    id        TEXT PRIMARY KEY NOT NULL,                   -- node id
    server_id TEXT,                                        -- server provider id
    -- Connection details (FR-14)
    ip_address        TEXT             NOT NULL,
    server_public_key TEXT             NOT NULL UNIQUE,    -- Must be unique
    port              INTEGER          NOT NULL DEFAULT 51820,

    -- State management (FR-14)
    status            TEXT             NOT NULL CHECK (status IN ('provisioning', 'active', 'destroying')),
    version           INTEGER          NOT NULL DEFAULT 1, -- Optimistic locking (FR-16)

    -- Timestamps (FR-14, FR-18)
    created_at        DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
    destroy_at        DATETIME,                            -- Scheduled destruction (FR-7)


    -- Activity tracking (FR-10, FR-11)
    last_handshake_at DATETIME,                            -- Last client activity
    connected_clients INTEGER          NOT NULL DEFAULT 0  -- Active client count
);

-- Performance indexes
CREATE INDEX idx_nodes_status ON nodes (status);
CREATE INDEX idx_nodes_destroy_at ON nodes (destroy_at) WHERE destroy_at IS NOT NULL;
CREATE INDEX idx_nodes_created_at ON nodes (created_at DESC);
CREATE INDEX idx_nodes_last_handshake ON nodes (last_handshake_at) WHERE last_handshake_at IS NOT NULL;

-- Trigger to auto-update updated_at (FR-18)
CREATE TRIGGER update_nodes_timestamp
    AFTER UPDATE
    ON nodes
    FOR EACH ROW
BEGIN
    UPDATE nodes SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
-- ============================================================================
-- PEER MANAGEMENT TABLES
-- ============================================================================

-- Peer configurations and IP allocations
CREATE TABLE peers
(
    id                TEXT PRIMARY KEY,         -- UUID
    node_id           TEXT     NOT NULL,        -- Foreign key to nodes.id
    public_key        TEXT     NOT NULL UNIQUE, -- Client's WireGuard public key
    allocated_ip      TEXT     NOT NULL UNIQUE, -- Assigned IP address (e.g., "10.8.1.5")
    preshared_key     TEXT,                     -- Optional preshared key
    status            TEXT     NOT NULL CHECK (status IN ('active', 'disconnected', 'removing')),
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_handshake_at DATETIME,                 -- Last activity timestamp

    FOREIGN KEY (node_id) REFERENCES nodes (id) ON DELETE CASCADE
);

-- IP subnet allocation per node
CREATE TABLE node_subnets
(
    node_id        TEXT PRIMARY KEY,         -- Foreign key to nodes.id
    subnet_cidr    TEXT     NOT NULL UNIQUE, -- e.g., "10.8.1.0/24"
    gateway_ip     TEXT     NOT NULL,        -- Server IP (e.g., "10.8.1.1")
    ip_range_start TEXT     NOT NULL,        -- First client IP (e.g., "10.8.1.2")
    ip_range_end   TEXT     NOT NULL,        -- Last client IP (e.g., "10.8.1.254")
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (node_id) REFERENCES nodes (id) ON DELETE CASCADE
);

-- Performance indexes for peers table
CREATE INDEX idx_peers_node_id ON peers (node_id);
CREATE INDEX idx_peers_public_key ON peers (public_key);
CREATE INDEX idx_peers_status ON peers (status);
CREATE INDEX idx_peers_allocated_ip ON peers (allocated_ip);

-- Performance indexes for node_subnets table
CREATE INDEX idx_node_subnets_subnet_cidr ON node_subnets (subnet_cidr);

-- Trigger to auto-update peers updated_at timestamp
CREATE TRIGGER update_peers_timestamp
    AFTER UPDATE
    ON peers
    FOR EACH ROW
BEGIN
    UPDATE peers SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;