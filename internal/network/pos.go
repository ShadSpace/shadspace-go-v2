package network

import (
	"fmt"
	"log"
	"time"
)

// PoSManager manages the Proof of Stake consensus
type PoSManager struct {
	node          *DecentralizedNode
	validatorMgr  *ValidatorManager
	proofVerifier *ProofVerifier
	isRunning     bool
}

// NewPoSManager creates a new PoS manager
func NewPoSManager(node *DecentralizedNode) *PoSManager {
	validatorMgr := NewValidatorManager(node)

	// Initialize proof verifier
	proofVerifier, err := NewProofVerifier(node, validatorMgr)
	if err != nil {
		log.Printf("Warning: failed to initialize proof verifier: %v", err)
	}

	return &PoSManager{
		node:          node,
		validatorMgr:  validatorMgr,
		proofVerifier: proofVerifier,
	}
}

// Start starts the PoS consensus system
func (pos *PoSManager) Start() error {
	if pos.isRunning {
		return nil
	}

	log.Println("Starting Proof of Stake consensus system...")

	// Start validator manager
	if err := pos.validatorMgr.Start(); err != nil {
		return fmt.Errorf("failed to start validator manager: %w", err)
	}

	pos.isRunning = true

	// Start periodic proof generation for locally stored files
	go pos.generatePeriodicProofs()

	// Start committee monitoring
	go pos.monitorCommittee()

	log.Println("Proof of Stake consensus system started successfully")
	return nil
}

// generatePeriodicProofs generates storage proofs for locally stored files
func (pos *PoSManager) generatePeriodicProofs() {
	// Initial proof generation after startup
	time.Sleep(30 * time.Second) // Wait for node to fully initialize
	pos.generateProofsForLocalFiles()

	// Periodic proof generation
	ticker := time.NewTicker(6 * time.Hour) // Generate proofs every 6 hours
	defer ticker.Stop()

	for {
		select {
		case <-pos.node.Ctx().Done():
			return
		case <-ticker.C:
			pos.generateProofsForLocalFiles()
		}
	}
}

// generateProofsForLocalFiles generates proofs for all locally stored files
func (pos *PoSManager) generateProofsForLocalFiles() {
	if pos.proofVerifier == nil || !pos.proofVerifier.IsInitialized() {
		log.Printf("Proof verifier not available, skipping proof generation")
		return
	}

	localFiles := pos.node.GetFileManager().ListFiles()

	proofCount := 0
	for _, metadata := range localFiles {
		proof, err := pos.proofVerifier.GenerateStorageProof(metadata.FileHash)
		if err != nil {
			log.Printf("Failed to generate proof for %s: %v", metadata.FileHash, err)
			continue
		}

		// Submit proof to network
		if err := pos.proofVerifier.SubmitProof(proof); err != nil {
			log.Printf("Failed to submit proof for %s: %v", metadata.FileHash, err)
			continue
		}

		proofCount++
	}

	if proofCount > 0 {
		log.Printf("Generated and submitted storage proofs for %d local files", proofCount)
	}
}

// monitorCommittee monitors validator committee status and performs actions
func (pos *PoSManager) monitorCommittee() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-pos.node.Ctx().Done():
			return
		case <-ticker.C:
			pos.checkValidatorStatus()
		}
	}
}

// checkValidatorStatus checks if this node is a validator and performs validator duties
func (pos *PoSManager) checkValidatorStatus() {
	if pos.validatorMgr.IsValidator() {
		log.Printf("✅ This node is an active validator in the current committee")
		// Perform validator-specific tasks here
		// - Monitor network for proof submissions
		// - Participate in consensus decisions
		// - Validate incoming proofs from other nodes
	} else {
		log.Printf("📝 This node is a regular farmer (not a validator)")
	}
}

// GetValidatorStatus returns the current validator status
func (pos *PoSManager) GetValidatorStatus() map[string]interface{} {
	status := make(map[string]interface{})

	status["is_running"] = pos.isRunning
	status["is_validator"] = pos.validatorMgr.IsValidator()

	committee := pos.validatorMgr.GetCurrentCommittee()
	if committee != nil {
		status["committee_size"] = len(committee.Validators)
		status["cycle_id"] = committee.CycleID
		status["cycle_start"] = committee.StartTime
		status["cycle_end"] = committee.EndTime

		// Find our position in the committee
		nodeID := pos.node.Node().Host.ID()
		for i, validator := range committee.Validators {
			if validator.PeerID == nodeID {
				status["validator_rank"] = i + 1
				status["validator_stake"] = validator.StakeAmount.String()
				status["validator_reputation"] = validator.Reputation
				status["validator_weight"] = validator.Weight
				break
			}
		}
	} else {
		status["committee_size"] = 0
		status["cycle_id"] = 0
		status["cycle_start"] = time.Time{}
		status["cycle_end"] = time.Time{}
	}

	// Add proof verifier status
	if pos.proofVerifier != nil {
		status["proof_verifier_initialized"] = pos.proofVerifier.IsInitialized()
	} else {
		status["proof_verifier_initialized"] = false
	}

	return status
}

// VerifyRemoteProof verifies a proof received from another peer
func (pos *PoSManager) VerifyRemoteProof(proof *StorageProof) (*VerificationResult, error) {
	if pos.proofVerifier == nil {
		return nil, fmt.Errorf("proof verifier not initialized")
	}

	return pos.proofVerifier.VerifyStorageProof(proof)
}

// SubmitStorageProof generates and submits a proof for a specific file
func (pos *PoSManager) SubmitStorageProof(fileHash string) error {
	if pos.proofVerifier == nil {
		return fmt.Errorf("proof verifier not initialized")
	}

	proof, err := pos.proofVerifier.GenerateStorageProof(fileHash)
	if err != nil {
		return fmt.Errorf("failed to generate proof: %w", err)
	}

	return pos.proofVerifier.SubmitProof(proof)
}

// ForceCommitteeUpdate forces a new validator committee selection
func (pos *PoSManager) ForceCommitteeUpdate() error {
	// This would typically be called by governance or time-based triggers
	log.Println("Forcing validator committee update...")
	// In a full implementation, this would trigger a new selection round
	return nil
}

// GetNetworkStats returns network statistics related to consensus
func (pos *PoSManager) GetNetworkStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Get basic network stats
	networkStats := pos.node.GetNetworkStats()
	stats["total_peers"] = networkStats["total_peers"]
	stats["known_files"] = networkStats["known_files"]
	stats["avg_reputation"] = networkStats["avg_reputation"]

	// Add consensus-specific stats
	committee := pos.validatorMgr.GetCurrentCommittee()
	if committee != nil {
		stats["active_validators"] = len(committee.Validators)

		// Calculate committee statistics
		var totalStake int64
		var totalReputation float64
		activeCount := 0

		for _, validator := range committee.Validators {
			if validator.IsActive {
				activeCount++
				totalStake += validator.StakeAmount.Int64()
				totalReputation += validator.Reputation
			}
		}

		stats["active_validator_count"] = activeCount
		if activeCount > 0 {
			stats["avg_validator_stake"] = totalStake / int64(activeCount)
			stats["avg_validator_reputation"] = totalReputation / float64(activeCount)
		}
	}

	return stats
}

// Stop stops the PoS consensus system
func (pos *PoSManager) Stop() {
	if !pos.isRunning {
		return
	}

	pos.validatorMgr.Stop()
	pos.isRunning = false
	log.Println("Proof of Stake consensus system stopped")
}
