package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeerManagement(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create a test node first
	nodeID := uuid.New().String()
	node, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              nodeID,
		IpAddress:       "203.0.113.1",
		ServerPublicKey: "test-public-key-" + nodeID,
		Port:            51820,
		Status:          "active",
	})
	require.NoError(t, err)
	assert.Equal(t, nodeID, node.ID)

	// Create a node subnet
	subnet, err := store.CreateNodeSubnet(ctx, CreateNodeSubnetParams{
		NodeID:       nodeID,
		SubnetCidr:   "10.8.1.0/24",
		GatewayIp:    "10.8.1.1",
		IpRangeStart: "10.8.1.2",
		IpRangeEnd:   "10.8.1.254",
	})
	require.NoError(t, err)
	assert.Equal(t, nodeID, subnet.NodeID)
	assert.Equal(t, "10.8.1.0/24", subnet.SubnetCidr)

	// Create a test peer
	peerID := uuid.New().String()
	peer, err := store.CreatePeer(ctx, CreatePeerParams{
		ID:          peerID,
		NodeID:      nodeID,
		PublicKey:   "test-peer-public-key-" + peerID,
		AllocatedIp: "10.8.1.2",
		Status:      "active",
	})
	require.NoError(t, err)
	assert.Equal(t, peerID, peer.ID)
	assert.Equal(t, nodeID, peer.NodeID)
	assert.Equal(t, "10.8.1.2", peer.AllocatedIp)

	// Test peer retrieval
	retrievedPeer, err := store.GetPeer(ctx, peerID)
	require.NoError(t, err)
	assert.Equal(t, peer.ID, retrievedPeer.ID)
	assert.Equal(t, peer.PublicKey, retrievedPeer.PublicKey)

	// Test peer retrieval by public key
	peerByKey, err := store.GetPeerByPublicKey(ctx, peer.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, peer.ID, peerByKey.ID)

	// Test peers by node
	peers, err := store.GetPeersByNode(ctx, nodeID)
	require.NoError(t, err)
	assert.Len(t, peers, 1)
	assert.Equal(t, peerID, peers[0].ID)

	// Test peer count
	count, err := store.CountPeersByNode(ctx, nodeID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Test active peer count
	activeCount, err := store.CountActivePeersByNode(ctx, nodeID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), activeCount)

	// Test allocated IPs
	allocatedIPs, err := store.GetAllocatedIPsByNode(ctx, nodeID)
	require.NoError(t, err)
	assert.Len(t, allocatedIPs, 1)
	assert.Equal(t, "10.8.1.2", allocatedIPs[0])

	// Test IP conflict check
	ipExists, err := store.CheckIPConflict(ctx, "10.8.1.2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), ipExists)

	ipNotExists, err := store.CheckIPConflict(ctx, "10.8.1.3")
	require.NoError(t, err)
	assert.Equal(t, int64(0), ipNotExists)

	// Test public key conflict check
	keyExists, err := store.CheckPublicKeyConflict(ctx, peer.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, int64(1), keyExists)

	// Test peer status update
	err = store.UpdatePeerStatus(ctx, UpdatePeerStatusParams{
		ID:     peerID,
		Status: "disconnected",
	})
	require.NoError(t, err)

	// Verify status update
	updatedPeer, err := store.GetPeer(ctx, peerID)
	require.NoError(t, err)
	assert.Equal(t, "disconnected", updatedPeer.Status)

	// Test peer deletion
	err = store.DeletePeer(ctx, peerID)
	require.NoError(t, err)

	// Verify peer is deleted
	_, err = store.GetPeer(ctx, peerID)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrNoRows, err)
}

func TestNodeSubnetManagement(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create a test node first
	nodeID := uuid.New().String()
	_, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              nodeID,
		IpAddress:       "203.0.113.1",
		ServerPublicKey: "test-public-key-" + nodeID,
		Port:            51820,
		Status:          "active",
	})
	require.NoError(t, err)

	// Test subnet creation
	subnet, err := store.CreateNodeSubnet(ctx, CreateNodeSubnetParams{
		NodeID:       nodeID,
		SubnetCidr:   "10.8.2.0/24",
		GatewayIp:    "10.8.2.1",
		IpRangeStart: "10.8.2.2",
		IpRangeEnd:   "10.8.2.254",
	})
	require.NoError(t, err)
	assert.Equal(t, nodeID, subnet.NodeID)
	assert.Equal(t, "10.8.2.0/24", subnet.SubnetCidr)

	// Test subnet retrieval
	retrievedSubnet, err := store.GetNodeSubnet(ctx, nodeID)
	require.NoError(t, err)
	assert.Equal(t, subnet.NodeID, retrievedSubnet.NodeID)
	assert.Equal(t, subnet.SubnetCidr, retrievedSubnet.SubnetCidr)

	// Test subnet retrieval by CIDR
	subnetByCIDR, err := store.GetNodeSubnetBySubnetCIDR(ctx, "10.8.2.0/24")
	require.NoError(t, err)
	assert.Equal(t, nodeID, subnetByCIDR.NodeID)

	// Test get all subnets
	allSubnets, err := store.GetAllNodeSubnets(ctx)
	require.NoError(t, err)
	assert.Len(t, allSubnets, 1)
	assert.Equal(t, nodeID, allSubnets[0].NodeID)

	// Test get used subnet CIDRs
	usedCIDRs, err := store.GetUsedSubnetCIDRs(ctx)
	require.NoError(t, err)
	assert.Len(t, usedCIDRs, 1)
	assert.Equal(t, "10.8.2.0/24", usedCIDRs[0])

	// Test subnet deletion
	err = store.DeleteNodeSubnet(ctx, nodeID)
	require.NoError(t, err)

	// Verify subnet is deleted
	_, err = store.GetNodeSubnet(ctx, nodeID)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrNoRows, err)
}

func TestPeerMigration(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create two test nodes
	nodeID1 := uuid.New().String()
	nodeID2 := uuid.New().String()

	_, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              nodeID1,
		IpAddress:       "203.0.113.1",
		ServerPublicKey: "test-public-key-1",
		Port:            51820,
		Status:          "active",
	})
	require.NoError(t, err)

	_, err = store.CreateNode(ctx, CreateNodeParams{
		ID:              nodeID2,
		IpAddress:       "203.0.113.2",
		ServerPublicKey: "test-public-key-2",
		Port:            51820,
		Status:          "active",
	})
	require.NoError(t, err)

	// Create a peer on node1
	peerID := uuid.New().String()
	_, err = store.CreatePeer(ctx, CreatePeerParams{
		ID:          peerID,
		NodeID:      nodeID1,
		PublicKey:   "test-peer-public-key",
		AllocatedIp: "10.8.1.2",
		Status:      "active",
	})
	require.NoError(t, err)

	// Test getting peers for migration
	peersForMigration, err := store.GetPeersForMigration(ctx, nodeID1)
	require.NoError(t, err)
	assert.Len(t, peersForMigration, 1)
	assert.Equal(t, peerID, peersForMigration[0].ID)

	// Test peer migration
	err = store.UpdatePeerNode(ctx, UpdatePeerNodeParams{
		ID:          peerID,
		NodeID:      nodeID2,
		AllocatedIp: "10.8.2.2",
	})
	require.NoError(t, err)

	// Verify migration
	migratedPeer, err := store.GetPeer(ctx, peerID)
	require.NoError(t, err)
	assert.Equal(t, nodeID2, migratedPeer.NodeID)
	assert.Equal(t, "10.8.2.2", migratedPeer.AllocatedIp)

	// Verify old node has no peers
	peersOnNode1, err := store.GetPeersByNode(ctx, nodeID1)
	require.NoError(t, err)
	assert.Len(t, peersOnNode1, 0)

	// Verify new node has the peer
	peersOnNode2, err := store.GetPeersByNode(ctx, nodeID2)
	require.NoError(t, err)
	assert.Len(t, peersOnNode2, 1)
	assert.Equal(t, peerID, peersOnNode2[0].ID)
}

func TestPeerStatistics(t *testing.T) {
	_, store := NewTestDB(t)
	ctx := context.Background()

	// Create a test node
	nodeID := uuid.New().String()
	_, err := store.CreateNode(ctx, CreateNodeParams{
		ID:              nodeID,
		IpAddress:       "203.0.113.1",
		ServerPublicKey: "test-public-key",
		Port:            51820,
		Status:          "active",
	})
	require.NoError(t, err)

	// Create peers with different statuses
	peers := []struct {
		id     string
		status string
	}{
		{uuid.New().String(), "active"},
		{uuid.New().String(), "active"},
		{uuid.New().String(), "disconnected"},
		{uuid.New().String(), "removing"},
	}

	for i, p := range peers {
		_, err := store.CreatePeer(ctx, CreatePeerParams{
			ID:          p.id,
			NodeID:      nodeID,
			PublicKey:   "test-peer-key-" + p.id,
			AllocatedIp: "10.8.1." + string(rune(i+2)),
			Status:      p.status,
		})
		require.NoError(t, err)
	}

	// Test peer statistics
	stats, err := store.GetPeerStatistics(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(4), stats.TotalPeers)
	assert.True(t, stats.ActivePeers.Valid)
	assert.Equal(t, float64(2), stats.ActivePeers.Float64)
	assert.True(t, stats.DisconnectedPeers.Valid)
	assert.Equal(t, float64(1), stats.DisconnectedPeers.Float64)
	assert.True(t, stats.RemovingPeers.Valid)
	assert.Equal(t, float64(1), stats.RemovingPeers.Float64)
}
