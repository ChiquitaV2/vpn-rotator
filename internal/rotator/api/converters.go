package api

import (
	"github.com/chiquitav2/vpn-rotator/internal/rotator/services"
	pkgapi "github.com/chiquitav2/vpn-rotator/pkg/api"
)

// ConvertToServiceConnectRequest maps API-layer ConnectRequest to service-layer ConnectRequest.
func ConvertToServiceConnectRequest(in pkgapi.ConnectRequest) services.ConnectRequest {
	return services.ConnectRequest{
		PublicKey:    in.PublicKey,
		GenerateKeys: in.GenerateKeys,
	}
}

// ConvertToAPIConnectResponse maps service-layer ConnectResponse to API-layer ConnectResponse.
func ConvertToAPIConnectResponse(in *services.ConnectResponse) *pkgapi.ConnectResponse {
	if in == nil {
		return nil
	}
	return &pkgapi.ConnectResponse{
		PeerID:           in.PeerID,
		ServerPublicKey:  in.ServerPublicKey,
		ServerIP:         in.ServerIP,
		ServerPort:       in.ServerPort,
		ClientIP:         in.ClientIP,
		ClientPrivateKey: in.ClientPrivateKey,
		DNS:              in.DNS,
		AllowedIPs:       in.AllowedIPs,
	}
}

// ConvertToAPIPeerStatus maps service-layer PeerStatus to API-layer Peer.
func ConvertToAPIPeerStatus(in *services.PeerStatus) *pkgapi.Peer {
	if in == nil {
		return nil
	}
	return &pkgapi.Peer{
		ID:              in.PeerID,
		PublicKey:       in.PublicKey,
		AllocatedIP:     in.AllocatedIP,
		Status:          in.Status,
		NodeID:          in.NodeID,
		ServerIP:        in.ServerIP,
		ServerStatus:    in.ServerStatus,
		CreatedAt:       in.ConnectedAt,
		LastHandshakeAt: nil, // PeerStatus doesn't have handshake info
	}
}

// ConvertToServicePeerListParams maps API-layer PeerListParams to service-layer PeerListParams.
func ConvertToServicePeerListParams(in pkgapi.PeerListParams) services.PeerListParams {
	return services.PeerListParams{
		NodeID: in.NodeID,
		Status: in.Status,
		Limit:  in.Limit,
		Offset: in.Offset,
	}
}

// ConvertToAPIPeersListResponse maps service-layer PeersListResponse to API-layer PeersList.
func ConvertToAPIPeersListResponse(in *services.PeersListResponse) *pkgapi.PeersList {
	if in == nil {
		return nil
	}
	out := &pkgapi.PeersList{
		Peers:      make([]pkgapi.Peer, 0, len(in.Peers)),
		TotalCount: in.TotalCount,
		Offset:     in.Offset,
		Limit:      in.Limit,
	}
	for _, p := range in.Peers {
		out.Peers = append(out.Peers, pkgapi.Peer{
			ID:              p.ID,
			NodeID:          p.NodeID,
			PublicKey:       p.PublicKey,
			AllocatedIP:     p.AllocatedIP,
			Status:          p.Status,
			CreatedAt:       p.CreatedAt,
			LastHandshakeAt: p.LastHandshakeAt,
		})
	}
	return out
}
