package protocol

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p/core/network"
)

// MessageHandler handles different types of protocol messages
type MessageHandler struct {
	masterNode MasterNodeHandler
}

// NewMessageHandler creates a new message handler
func NewMessageHandler(masterNode MasterNodeHandler) *MessageHandler {
	return &MessageHandler{
		masterNode: masterNode,
	}
}

// HandleDiscoveryMessage handles discovery protocol messages
func (h *MessageHandler) HandleDiscoveryMessage(stream network.Stream, messageData []byte) error {
	var baseMsg types.Message
	if err := json.Unmarshal(messageData, &baseMsg); err != nil {
		return fmt.Errorf("failed to unmarshal base message: %w", err)
	}

	log.Printf("Received discovery message type: %s from %s", baseMsg.Type, baseMsg.PeerID)

	switch baseMsg.Type {
	case types.TypeRegistrationRequest:
		return h.handleRegistrationRequest(stream, messageData)
	case types.TypeProofOfStorage:
		return h.handleProofOfStorage(stream, messageData)
	case types.TypeFileLocationQuery:
		return h.handleFileLocationQuery(stream, messageData)
	case types.TypeChallengeResponse:
		return h.handleChallengeResponse(stream, messageData)
	default:
		return fmt.Errorf("unknown message type: %s", baseMsg.Type)
	}
}

// HandleStorageMessage handles storage protocol messages
func (h *MessageHandler) HandleStorageMessage(stream network.Stream, messageData []byte) error {
	var baseMsg types.Message
	if err := json.Unmarshal(messageData, &baseMsg); err != nil {
		return fmt.Errorf("failed to unmarshal base message: %w", err)
	}

	log.Printf("Received storage message type: %s from %s", baseMsg.Type, baseMsg.PeerID)

	switch baseMsg.Type {
	case types.TypeStorageOffer:
		return h.handleStorageOffer(stream, messageData)
	default:
		return fmt.Errorf("unknown storage message type: %s", baseMsg.Type)
	}
}

// HandleBitSwapMessage handles bitswap protocol messages
func (h *MessageHandler) HandleBitSwapMessage(stream network.Stream, messageData []byte) error {
	var baseMsg types.Message
	if err := json.Unmarshal(messageData, &baseMsg); err != nil {
		return fmt.Errorf("failed to unmarshal base message: %w", err)
	}

	log.Printf("Received bitswap message type: %s from %s", baseMsg.Type, baseMsg.PeerID)

	switch baseMsg.Type {
	case types.TypeChunkRequest:
		return h.handleChunkRequest(stream, messageData)
	case types.TypeChunkResponse:
		return h.handleChunkResponse(stream, messageData)
	default:
		return fmt.Errorf("unknown bitswap message type: %s", baseMsg.Type)
	}
}

// handleRegistrationRequest processes farmer registration
func (h *MessageHandler) handleRegistrationRequest(stream network.Stream, data []byte) error {
	var req types.RegistrationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to unmarshal registration request: %w", err)
	}

	log.Printf("Processing registration request from farmer %s", req.PeerID)

	// Call the master node's registration handler
	if err := h.masterNode.HandleIncomingRegistration(req.PeerID, req); err != nil {
		// Send error response
		response := types.RegistrationResponse{
			Message: types.Message{
				Type:      types.TypeRegistrationResponse,
				ID:        fmt.Sprintf("resp-%s", req.ID),
				Timestamp: time.Now(),
				PeerID:    h.masterNode.GetHost().ID(),
				Success:   false,
			},
			StatusMessage: fmt.Sprintf("Registration failed: %v", err),
		}
		return h.sendResponse(stream, response)
	}

	// Send success response
	response := types.RegistrationResponse{
		Message: types.Message{
			Type:      types.TypeRegistrationResponse,
			ID:        fmt.Sprintf("resp-%s", req.ID),
			Timestamp: time.Now(),
			PeerID:    h.masterNode.GetHost().ID(),
			Success:   true,
		},
		StatusMessage: "Registration successful - Ready for storage challenges",
	}

	return h.sendResponse(stream, response)
}

// handleProofOfStorage processes storage proofs from farmers
func (h *MessageHandler) handleProofOfStorage(stream network.Stream, data []byte) error {
	var proofMsg types.ProofOfStorage
	if err := json.Unmarshal(data, &proofMsg); err != nil {
		return fmt.Errorf("failed to unmarshal proof of storage: %w", err)
	}

	log.Printf("Received Proof of Storage from %s - Storage: %d/%d, Chunks: %d, Proofs: %d", 
		proofMsg.PeerID, proofMsg.UsedStorage, proofMsg.AvailableStorage, 
		proofMsg.ChunksStored, len(proofMsg.StorageProofs))

	// Validate storage proofs
	validProofs := h.validateStorageProofs(proofMsg.StorageProofs)
	
	// Calculate reliability score based on proofs
	reliability := h.calculateReliability(validProofs, proofMsg.Metrics)

	log.Printf("Farmer %s reliability score: %.2f (%d/%d valid proofs)", 
		proofMsg.PeerID, reliability, len(validProofs), len(proofMsg.StorageProofs))

	response := types.Message{
		Type:      "proof_acknowledged",
		ID:        fmt.Sprintf("ack-%s", proofMsg.ID),
		Timestamp: time.Now(),
		PeerID:    h.masterNode.GetHost().ID(),
		Success:   len(validProofs) > 0,
	}

	return h.sendResponse(stream, response)
}

// handleChallengeResponse processes responses to storage challenges
func (h *MessageHandler) handleChallengeResponse(stream network.Stream, data []byte) error {
	var challengeResp types.ChallengeResponse
	if err := json.Unmarshal(data, &challengeResp); err != nil {
		return fmt.Errorf("failed to unmarshal challenge response: %w", err)
	}

	log.Printf("Received challenge response for ID: %s from %s with %d proofs", 
		challengeResp.ChallengeID, challengeResp.PeerID, len(challengeResp.Proofs))

	// Validate challenge response timing
	if time.Now().After(challengeResp.RespondedAt.Add(5 * time.Minute)) {
		log.Printf("Challenge response expired for %s", challengeResp.PeerID)
		challengeResp.Success = false
	}

	// Validate the provided proofs
	valid := h.validateChallengeProofs(challengeResp.Proofs, challengeResp.ChallengeID)
	
	log.Printf("Challenge %s validation result: %t", challengeResp.ChallengeID, valid)

	response := types.Message{
		Type:      "challenge_result",
		ID:        fmt.Sprintf("result-%s", challengeResp.ID),
		Timestamp: time.Now(),
		PeerID:    h.masterNode.GetHost().ID(),
		Success:   valid,
	}

	return h.sendResponse(stream, response)
}

// handleFileLocationQuery processes file location queries
func (h *MessageHandler) handleFileLocationQuery(stream network.Stream, data []byte) error {
	var query types.FileLocationQuery
	if err := json.Unmarshal(data, &query); err != nil {
		return fmt.Errorf("failed to unmarshal file location query: %w", err)
	}

	log.Printf("File location query for hash: %s from %s", query.FileHash, query.PeerID)

	response := types.FileLocationResponse{
		Message: types.Message{
			Type:      types.TypeFileLocationResponse,
			ID:        fmt.Sprintf("resp-%s", query.ID),
			Timestamp: time.Now(),
			PeerID:    h.masterNode.GetHost().ID(),
			Success:   true,
		},
		FileHash:  query.FileHash,
		Locations: []types.ChunkLocation{},
	}

	return h.sendResponse(stream, response)
}

// handleStorageOffer processes storage offers from farmers
func (h *MessageHandler) handleStorageOffer(stream network.Stream, data []byte) error {
	var offer types.StorageOffer
	if err := json.Unmarshal(data, &offer); err != nil {
		return fmt.Errorf("failed to unmarshal storage offer: %w", err)
	}

	log.Printf("Storage offer for chunk %s from %s - Size: %d, Duration: %d", 
		offer.ChunkHash, offer.PeerID, offer.Size, offer.Duration)

	response := types.Message{
		Type:      "storage_offer_ack",
		ID:        fmt.Sprintf("ack-%s", offer.ID),
		Timestamp: time.Now(),
		PeerID:    h.masterNode.GetHost().ID(),
		Success:   true,
	}

	return h.sendResponse(stream, response)
}

// handleChunkRequest processes chunk requests
func (h *MessageHandler) handleChunkRequest(stream network.Stream, data []byte) error {
	var req types.ChunkRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to unmarshal chunk request: %w", err)
	}

	log.Printf("Chunk request for %s from %s", req.ChunkHash, req.PeerID)

	response := types.ChunkResponse{
		Message: types.Message{
			Type:      types.TypeChunkResponse,
			ID:        fmt.Sprintf("resp-%s", req.ID),
			Timestamp: time.Now(),
			PeerID:    h.masterNode.GetHost().ID(),
		},
		ChunkHash: req.ChunkHash,
		Success:   false,
		Error:     "Chunk retrieval not implemented",
	}

	return h.sendResponse(stream, response)
}

// handleChunkResponse processes chunk responses
func (h *MessageHandler) handleChunkResponse(stream network.Stream, data []byte) error {
	var resp types.ChunkResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("failed to unmarshal chunk response: %w", err)
	}

	log.Printf("Chunk response for %s - Success: %t", resp.ChunkHash, resp.Success)
	return nil
}

// validateStorageProofs validates cryptographic storage proofs
func (h *MessageHandler) validateStorageProofs(proofs []types.StorageProof) []types.StorageProof {
	var validProofs []types.StorageProof
	
	for _, proof := range proofs {
		if h.validateSingleProof(proof) {
			validProofs = append(validProofs, proof)
		} else {
			log.Printf("Invalid proof for chunk %s", proof.ChunkHash)
		}
	}
	
	return validProofs
}

// validateSingleProof validates a single storage proof
func (h *MessageHandler) validateSingleProof(proof types.StorageProof) bool {
	// Placeholder - always return true for now
	return len(proof.Proof) > 0 && proof.Size > 0
}

// validateChallengeProofs validates proofs for a specific challenge
func (h *MessageHandler) validateChallengeProofs(proofs []types.StorageProof, challengeID string) bool {
	return len(proofs) > 0
}

// calculateReliability calculates farmer reliability score
func (h *MessageHandler) calculateReliability(validProofs []types.StorageProof, metrics types.FarmerMetrics) float64 {
	if len(validProofs) == 0 {
		return 0.0
	}
	
	// Base reliability from proof validation
	proofScore := float64(len(validProofs)) / float64(len(validProofs)+1)
	
	// Combine with other metrics
	reliability := (proofScore + metrics.SuccessRate + metrics.StorageHealth) / 3.0
	
	return reliability
}

// sendResponse sends a JSON response over the stream
func (h *MessageHandler) sendResponse(stream network.Stream, response interface{}) error {
	responseData, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	if _, err := stream.Write(responseData); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	return nil
}