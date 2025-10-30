-- ----------------------------------------------------------------------------
-- Node Retrieval
-- ----------------------------------------------------------------------------

-- name: GetNode :one
-- Get a specific node by ID
SELECT * FROM nodes WHERE id = ? LIMIT 1;

-- name: GetNodeByIP :one
-- Get a specific node by IP address
SELECT *
FROM nodes
WHERE ip_address = ? LIMIT 1;

-- name: GetActiveNode :one
-- Get the currently active node (FR-17)
-- Returns the most recently created active node to handle transition edge cases
SELECT * FROM nodes
WHERE status = 'active'
ORDER BY created_at DESC
    LIMIT 1;

-- name: GetNodesByStatus :many
-- Get all nodes with a specific status
SELECT * FROM nodes
WHERE status = ?
ORDER BY created_at DESC;

-- name: GetAllNodes :many
-- Get all nodes (for debugging/admin)
SELECT * FROM nodes
ORDER BY created_at DESC;

-- name: GetLatestNode :one
-- Get the most recently created node regardless of status
SELECT * FROM nodes
ORDER BY created_at DESC
    LIMIT 1;

-- ----------------------------------------------------------------------------
-- Node Creation & Updates
-- ----------------------------------------------------------------------------

-- name: CreateNode :one
-- Create a new node (FR-3, FR-9)
INSERT INTO nodes (
    id,
    ip_address,
    server_public_key,
    port,
    status,
    created_at
)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
    RETURNING *;

-- name: UpdateNodeStatus :exec
-- Update node status with optimistic locking (FR-16)
-- Returns error if version doesn't match (concurrent modification)
UPDATE nodes
SET
    status = ?,
    version = version + 1
WHERE id = ? AND version = ?;

-- name: ScheduleNodeDestruction :exec
-- Mark node for destruction with grace period (FR-7, FR-8)
UPDATE nodes
SET
    status = 'destroying',
    destroy_at = ?,
    version = version + 1
WHERE id = ? AND version = ?;

-- name: CancelNodeDestruction :exec
-- Cancel scheduled destruction (FR-13)
UPDATE nodes
SET
    status = 'active',
    destroy_at = NULL,
    version = version + 1
WHERE id = ? AND version = ?;

-- name: UpdateNodeActivity :exec
-- Update last handshake time and client count (FR-10, FR-11)
UPDATE nodes
SET
    last_handshake_at = ?,
    connected_clients = ?
WHERE id = ?;

-- name: UpdateNodeDetails :exec
-- Update node IP address and public key while keeping provisioning status
UPDATE nodes
SET server_id         = ?,
    ip_address        = ?,
    server_public_key = ?,
    version           = version + 1
WHERE id = ?
  AND status = 'provisioning'
  AND version = ?;

-- name: MarkNodeActive :exec
-- Transition node from provisioning to active (FR-6)
UPDATE nodes
SET
    status = 'active',
    version = version + 1
WHERE id = ? AND status = 'provisioning' AND version = ?;

-- name: DeleteNode :exec
-- Permanently delete a node (FR-8, FR-25)
DELETE FROM nodes WHERE id = ?;

-- ----------------------------------------------------------------------------
-- Rotation & Cleanup Queries
-- ----------------------------------------------------------------------------

-- name: GetNodesDueForRotation :many
-- Get nodes that need rotation (FR-2)
-- Active nodes older than rotation interval
SELECT * FROM nodes
WHERE status = 'active'
  AND datetime(created_at, '+' || ? || ' hours') <= CURRENT_TIMESTAMP
  AND connected_clients > 0;

-- name: GetNodesForDestruction :many
-- Get nodes whose grace period has expired (FR-8)
SELECT * FROM nodes
WHERE status = 'destroying'
  AND destroy_at IS NOT NULL
  AND destroy_at <= CURRENT_TIMESTAMP;

-- name: GetIdleNodes :many
-- Get nodes with no activity for idle timeout period (FR-12)
-- Considers a node idle if:
-- 1. No clients connected, OR
-- 2. Last handshake was more than idle_minutes ago
SELECT * FROM nodes
WHERE status IN ('active', 'destroying')
  AND (
    connected_clients = 0
        OR last_handshake_at IS NULL
        OR datetime(last_handshake_at, '+' || ? || ' minutes') <= CURRENT_TIMESTAMP
    );

-- name: GetOrphanedNodes :many
-- Get nodes stuck in non-terminal states (FR-25)
-- Provisioning for >1 hour or destroying for >1 hour
SELECT * FROM nodes
WHERE (
          (status = 'provisioning' AND datetime(created_at, '+1 hour') <= CURRENT_TIMESTAMP)
              OR (status = 'destroying' AND destroy_at IS NOT NULL AND datetime(destroy_at, '+1 hour') <= CURRENT_TIMESTAMP)
          );

-- name: GetNodesScheduledForDestruction :many
-- Get all nodes with scheduled destruction (for monitoring)
SELECT * FROM nodes
WHERE destroy_at IS NOT NULL
ORDER BY destroy_at ASC;

-- ----------------------------------------------------------------------------
-- Health & Statistics
-- ----------------------------------------------------------------------------

-- name: GetNodeCount :one
-- Count nodes by status
SELECT
    COUNT(*) as total,
    SUM(CASE WHEN status = 'provisioning' THEN 1 ELSE 0 END) as provisioning,
    SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active,
    SUM(CASE WHEN status = 'destroying' THEN 1 ELSE 0 END) as destroying
FROM nodes;

-- name: GetTotalConnectedClients :one
-- Sum of all connected clients across all nodes
SELECT COALESCE(SUM(connected_clients), 0) as total_clients
FROM nodes
WHERE status = 'active';

-- name: HasActiveNode :one
-- Quick check if any active node exists (FR-17, FR-20)
SELECT EXISTS(SELECT 1 FROM nodes WHERE status = 'active') as has_active;

-- ----------------------------------------------------------------------------
-- Atomic Operations (Use with transactions)
-- ----------------------------------------------------------------------------

-- name: BeginRotation :exec
-- Mark old node for destruction during rotation
-- USE IN TRANSACTION with CreateNode
UPDATE nodes
SET
    status = 'destroying',
    destroy_at = datetime(CURRENT_TIMESTAMP, '+' || ? || ' hours'),
    version = version + 1
WHERE id = ? AND status = 'active' AND version = ?;

-- name: GetNodeForUpdate :one
-- Get node with lock for update (use in transaction)
-- SQLite doesn't have SELECT FOR UPDATE, but this documents intent
SELECT * FROM nodes WHERE id = ? LIMIT 1;

-- ============================================================================
-- MIGRATION HELPERS
-- ============================================================================

-- name: CleanupAllNodes :exec
-- DANGEROUS: Delete all nodes (for testing/reset only)
DELETE FROM nodes;

-- name: GetDatabaseVersion :one
-- Get schema version for migrations
SELECT sqlite_version() as version;

-- ============================================================================
-- PEER MANAGEMENT QUERIES
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Peer Retrieval
-- ----------------------------------------------------------------------------

-- name: GetPeer :one
-- Get a specific peer by ID
SELECT *
FROM peers
WHERE id = ? LIMIT 1;

-- name: GetPeerByPublicKey :one
-- Get a peer by public key
SELECT *
FROM peers
WHERE public_key = ? LIMIT 1;

-- name: GetPeersByNode :many
-- Get all peers for a specific node
SELECT *
FROM peers
WHERE node_id = ?
ORDER BY created_at DESC;

-- name: GetPeersByStatus :many
-- Get all peers with a specific status
SELECT *
FROM peers
WHERE status = ?
ORDER BY created_at DESC;

-- name: GetAllPeers :many
-- Get all peers
SELECT *
FROM peers
ORDER BY created_at DESC;

-- name: CountPeersByNode :one
-- Count peers for a specific node
SELECT COUNT(*) as count
FROM peers
WHERE node_id = ?;

-- name: CountActivePeersByNode :one
-- Count active peers for a specific node
SELECT COUNT(*) as count
FROM peers
WHERE node_id = ? AND status = 'active';

-- name: GetAllocatedIPsByNode :many
-- Get all allocated IPs for a specific node
SELECT allocated_ip
FROM peers
WHERE node_id = ?
ORDER BY allocated_ip;

-- ----------------------------------------------------------------------------
-- Peer Creation & Updates
-- ----------------------------------------------------------------------------

-- name: CreatePeer :one
-- Create a new peer
INSERT INTO peers (id,
                   node_id,
                   public_key,
                   allocated_ip,
                   preshared_key,
                   status,
                   created_at)
VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP) RETURNING *;

-- name: UpdatePeerStatus :exec
-- Update peer status
UPDATE peers
SET status = ?
WHERE id = ?;

-- name: UpdatePeerLastHandshake :exec
-- Update peer last handshake time
UPDATE peers
SET last_handshake_at = ?
WHERE id = ?;

-- name: DeletePeer :exec
-- Delete a peer
DELETE
FROM peers
WHERE id = ?;

-- name: DeletePeersByNode :exec
-- Delete all peers for a specific node
DELETE
FROM peers
WHERE node_id = ?;

-- ----------------------------------------------------------------------------
-- Node Subnet Queries
-- ----------------------------------------------------------------------------

-- name: GetNodeSubnet :one
-- Get subnet information for a specific node
SELECT *
FROM node_subnets
WHERE node_id = ? LIMIT 1;

-- name: GetAllNodeSubnets :many
-- Get all node subnets
SELECT *
FROM node_subnets
ORDER BY created_at DESC;

-- name: GetNodeSubnetBySubnetCIDR :one
-- Get node subnet by CIDR
SELECT *
FROM node_subnets
WHERE subnet_cidr = ? LIMIT 1;

-- name: CreateNodeSubnet :one
-- Create a new node subnet allocation
INSERT INTO node_subnets (node_id,
                          subnet_cidr,
                          gateway_ip,
                          ip_range_start,
                          ip_range_end,
                          created_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP) RETURNING *;

-- name: DeleteNodeSubnet :exec
-- Delete a node subnet
DELETE
FROM node_subnets
WHERE node_id = ?;

-- name: GetUsedSubnetCIDRs :many
-- Get all used subnet CIDRs
SELECT subnet_cidr
FROM node_subnets
ORDER BY subnet_cidr;

-- ----------------------------------------------------------------------------
-- Peer Management Operations
-- ----------------------------------------------------------------------------

-- name: GetPeersForMigration :many
-- Get active peers that need to be migrated from a node
SELECT *
FROM peers
WHERE node_id = ?
  AND status = 'active'
ORDER BY created_at ASC;

-- name: UpdatePeerNode :exec
-- Move a peer to a different node (for migration)
UPDATE peers
SET node_id      = ?,
    allocated_ip = ?
WHERE id = ?;

-- name: GetInactivePeers :many
-- Get peers that have been inactive for a specified duration
SELECT *
FROM peers
WHERE status = 'active'
  AND (
    last_handshake_at IS NULL
        OR datetime(last_handshake_at, '+' || ? || ' minutes') <= CURRENT_TIMESTAMP
    )
ORDER BY last_handshake_at ASC;

-- name: GetPeerStatistics :one
-- Get peer statistics across all nodes
SELECT COUNT(*)                                                 as total_peers,
       SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END)       as active_peers,
       SUM(CASE WHEN status = 'disconnected' THEN 1 ELSE 0 END) as disconnected_peers,
       SUM(CASE WHEN status = 'removing' THEN 1 ELSE 0 END)     as removing_peers
FROM peers;

-- name: CheckIPConflict :one
-- Check if an IP address is already allocated
SELECT EXISTS(SELECT 1 FROM peers WHERE allocated_ip = ?) as ip_exists;

-- name: CheckPublicKeyConflict :one
-- Check if a public key is already in use
SELECT EXISTS(SELECT 1 FROM peers WHERE public_key = ?) as key_exists;