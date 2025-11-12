package api

import (
	"log/slog"
	"net/http"

	apperrors "github.com/chiquitav2/vpn-rotator/internal/shared/errors"
	"github.com/chiquitav2/vpn-rotator/pkg/api"
)

// connectHandler handles peer connection requests
func (s *Server) connectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		op := logger.StartOp(ctx, "connectHandler")

		var req api.ConnectRequest
		if err := ParseJSONRequest(r, &req); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}
		op.Progress("request parsed")

		if err := ValidateConnectRequest(&req); err != nil {
			op.Fail(err, "request validation failed")
			WriteErrorResponse(w, r, err)
			return
		}
		op.Progress("request validated")

		response, err := s.vpnService.ConnectPeer(ctx, req)
		if err != nil {
			op.Fail(err, "failed to connect peer")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("peer connected successfully", slog.String("peer_id", response.PeerID))
	}
}

// disconnectHandler handles peer disconnection requests
func (s *Server) disconnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		op := logger.StartOp(ctx, "disconnectHandler")

		var req api.DisconnectRequest
		if err := ParseJSONRequest(r, &req); err != nil {
			op.Fail(err, "failed to parse request")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := ValidateDisconnectRequest(&req); err != nil {
			op.Fail(err, "request validation failed")
			WriteErrorResponse(w, r, err)
			return
		}
		op.With("peer_id", req.PeerID)

		err := s.peerConnectionService.DisconnectPeer(ctx, req.PeerID)
		if err != nil {
			op.Fail(err, "failed to disconnect peer")
			WriteErrorResponse(w, r, err)
			return
		}

		response := api.DisconnectResponse{
			Message: "Peer disconnected successfully",
			PeerID:  req.PeerID,
		}

		if err := WriteSuccess(w, response); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("peer disconnected successfully")
	}
}

// listPeersHandler handles peer listing requests
func (s *Server) listPeersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		op := logger.StartOp(ctx, "listPeersHandler")

		params, err := ValidatePeerListParams(r)
		if err != nil {
			op.Fail(err, "failed to validate params")
			WriteErrorResponse(w, r, err)
			return
		}

		peers, err := s.peerConnectionService.ListPeers(ctx, params)
		if err != nil {
			op.Fail(err, "failed to list active peers")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, peers); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("listed peers successfully", slog.Int("count", len(peers.Peers)))
	}
}

// getPeerHandler handles individual peer status requests
func (s *Server) getPeerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := GetLogger(ctx)
		peerID := r.PathValue("peerID")
		op := logger.StartOp(ctx, "getPeerHandler", slog.String("peer_id", peerID))

		if peerID == "" {
			err := apperrors.NewDomainAPIError(apperrors.ErrCodeValidation, "Peer ID is required in URL path", false, nil)
			op.Fail(err, "missing peer id")
			WriteErrorResponse(w, r, err)
			return
		}

		peerStatus, err := s.peerConnectionService.GetPeerStatus(ctx, peerID)
		if err != nil {
			op.Fail(err, "failed to get peer status")
			WriteErrorResponse(w, r, err)
			return
		}

		if err := WriteSuccess(w, peerStatus); err != nil {
			op.Fail(err, "failed to encode response")
			return
		}
		op.Complete("retrieved peer status successfully")
	}
}
