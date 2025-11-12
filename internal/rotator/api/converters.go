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

// ConvertToAPIPeerStatus maps service-layer PeerStatus to API-layer PeerStatusResponse.
func ConvertToAPIPeerStatus(in *services.PeerStatus) *pkgapi.PeerStatusResponse {
	if in == nil {
		return nil
	}
	return &pkgapi.PeerStatusResponse{
		PeerID:       in.PeerID,
		PublicKey:    in.PublicKey,
		AllocatedIP:  in.AllocatedIP,
		Status:       in.Status,
		NodeID:       in.NodeID,
		ServerIP:     in.ServerIP,
		ServerStatus: in.ServerStatus,
		ConnectedAt:  in.ConnectedAt,
		LastSeen:     in.LastSeen,
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

// ConvertToAPIPeersListResponse maps service-layer PeersListResponse to API-layer PeersListResponse.
func ConvertToAPIPeersListResponse(in *services.PeersListResponse) *pkgapi.PeersListResponse {
	if in == nil {
		return nil
	}
	out := &pkgapi.PeersListResponse{
		Peers:      make([]pkgapi.PeerInfo, 0, len(in.Peers)),
		TotalCount: in.TotalCount,
		Offset:     in.Offset,
		Limit:      in.Limit,
	}
	for _, p := range in.Peers {
		out.Peers = append(out.Peers, pkgapi.PeerInfo{
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

// toAPIHealthResponse converts a services.HealthResponse into the API response type.
func toAPIHealthResponse(in *services.HealthResponse) *pkgapi.HealthResponse {
	if in == nil {
		return &pkgapi.HealthResponse{Status: "unknown"}
	}
	var prov *pkgapi.ProvisioningInfo
	if in.Provisioning != nil {
		prov = &pkgapi.ProvisioningInfo{
			IsActive:     in.Provisioning.IsActive,
			Phase:        in.Provisioning.Phase,
			Progress:     in.Provisioning.Progress,
			EstimatedETA: in.Provisioning.EstimatedETA,
		}
	}
	return &pkgapi.HealthResponse{
		Status:       in.Status,
		Version:      in.Version,
		Provisioning: prov,
	}
}
