-- Node status values: provisioning, active, destroying
-- Status transitions: provisioning -> active -> destroying -> (deleted)
CREATE TABLE nodes
(
    -- Identity
    id                TEXT PRIMARY KEY NOT NULL,           -- Hetzner server ID

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
