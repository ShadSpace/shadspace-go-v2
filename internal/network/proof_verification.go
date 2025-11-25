package network

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/crypto"
	"github.com/ShadSpace/shadspace-go-v2/internal/zksnark"

	libstream "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ProofVerifier handles zero-knowledge proof verification
type ProofVerifier struct {
	node        *DecentralizedNode
	cryptoMgr   *crypto.CryptoManager
	vm          *ValidatorManager
	zkProver    *zksnark.StorageProofProver
	initialized bool
}

// NewProofVerifier creates a new proof verifier
func NewProofVerifier(node *DecentralizedNode, vm *ValidatorManager) (*ProofVerifier, error) {
	// Initialize the zk-SNARK prover
	zkProver, err := zksnark.NewStorageProofProver()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize zk-SNARK prover: %w", err)
	}

	pv := &ProofVerifier{
		node:        node,
		cryptoMgr:   crypto.NewCryptoManager(),
		vm:          vm,
		zkProver:    zkProver,
		initialized: true,
	}

	// Register proof verification handler
	if err := pv.registerProofHandler(); err != nil {
		return nil, fmt.Errorf("failed to register proof handler: %w", err)
	}

	return pv, nil
}

// StorageProof represents a proof of storage
type StorageProof struct {
	FileHash      string
	ProofData     []byte
	PublicWitness []byte
	Timestamp     time.Time
	ProverID      peer.ID
	Challenge     []byte
	Response      []byte
	Signature     []byte
}

// VerificationResult represents the result of proof verification
type VerificationResult struct {
	Proof      *StorageProof
	IsValid    bool
	VerifiedBy peer.ID
	Timestamp  time.Time
	Error      string
}

// ProofRequest represents a request to verify a proof
type ProofRequest struct {
	Proof       *StorageProof
	RequestID   string
	RequesterID peer.ID
	Timestamp   time.Time
}

// ProofResponse represents the response to a proof verification request
type ProofResponse struct {
	RequestID  string
	ProofHash  string
	IsValid    bool
	VerifiedBy peer.ID
	Timestamp  time.Time
	Error      string
	Signature  []byte
}

// GenerateStorageProof generates a zk-SNARK proof that a file is stored
func (pv *ProofVerifier) GenerateStorageProof(fileHash string) (*StorageProof, error) {
	if !pv.initialized {
		return nil, fmt.Errorf("proof verifier not initialized")
	}

	// Check if file exists locally
	_, err := pv.node.GetFileMetadata(fileHash)
	if err != nil {
		return nil, fmt.Errorf("file not found locally: %w", err)
	}

	// Read the actual file data
	fileData, err := pv.node.GetFileManager().RetrieveFile(fileHash)
	if err != nil {
		return nil, fmt.Errorf("failed to read file data: %w", err)
	}

	// Generate random challenge
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Generate secret salt (in production, this would be stored securely)
	secretSalt := pv.generateSecretSalt(fileHash)

	// Generate zk-SNARK proof
	proofData, publicWitness, err := pv.zkProver.GenerateProof(
		[]byte(fileHash),
		challenge,
		fileData,
		secretSalt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate zk-SNARK proof: %w", err)
	}

	// Compute response for verification
	response := pv.zkProver.ComputeResponse(challenge, fileData, secretSalt)

	// Sign the proof
	signature, err := pv.cryptoMgr.Sign(proofData)
	if err != nil {
		return nil, fmt.Errorf("failed to sign proof: %w", err)
	}

	proof := &StorageProof{
		FileHash:      fileHash,
		ProofData:     proofData,
		PublicWitness: publicWitness,
		Timestamp:     time.Now(),
		ProverID:      pv.node.Node().Host.ID(),
		Challenge:     challenge,
		Response:      response.Bytes(),
		Signature:     signature,
	}

	return proof, nil
}

// generateSecretSalt generates a deterministic secret salt for a file
func (pv *ProofVerifier) generateSecretSalt(fileHash string) []byte {
	// In production, this would use a secure key derivation function
	// For now, we'll use a simple hash-based approach
	nodeID := pv.node.Node().Host.ID().String()
	saltInput := append([]byte(fileHash), []byte(nodeID)...)
	return pv.cryptoMgr.Hash(saltInput)
}

// VerifyStorageProof verifies a zk-SNARK storage proof submitted by a peer
func (pv *ProofVerifier) VerifyStorageProof(proof *StorageProof) (*VerificationResult, error) {
	if !pv.initialized {
		return &VerificationResult{
			Proof:      proof,
			IsValid:    false,
			VerifiedBy: pv.node.Node().Host.ID(),
			Timestamp:  time.Now(),
			Error:      "proof verifier not initialized",
		}, nil
	}

	// Verify signature first
	if err := pv.cryptoMgr.Verify(proof.ProverID, proof.ProofData, proof.Signature); err != nil {
		return &VerificationResult{
			Proof:      proof,
			IsValid:    false,
			VerifiedBy: pv.node.Node().Host.ID(),
			Timestamp:  time.Now(),
			Error:      fmt.Sprintf("signature verification failed: %v", err),
		}, nil
	}

	// Verify proof validity using zk-SNARK
	isValid := pv.verifyZKProof(proof)

	result := &VerificationResult{
		Proof:      proof,
		IsValid:    isValid,
		VerifiedBy: pv.node.Node().Host.ID(),
		Timestamp:  time.Now(),
	}

	if !isValid {
		result.Error = "zk-SNARK proof verification failed"
	}

	// Update reputation based on verification result
	if isValid {
		pv.node.GetReputation().RecordSuccessfulInteraction(proof.ProverID)
	} else {
		pv.node.GetReputation().RecordFailedInteraction(proof.ProverID)
	}

	return result, nil
}

// verifyZKProof verifies a zk-SNARK storage proof
func (pv *ProofVerifier) verifyZKProof(proof *StorageProof) bool {
	// Basic checks
	if proof.FileHash == "" || len(proof.ProofData) == 0 || len(proof.PublicWitness) == 0 {
		log.Printf("Invalid proof: missing required fields")
		return false
	}

	if time.Since(proof.Timestamp) > 24*time.Hour {
		log.Printf("Proof too old: %v", proof.Timestamp)
		return false // Proof too old
	}

	// Verify the zk-SNARK proof
	err := pv.zkProver.VerifyProof(proof.ProofData, proof.PublicWitness)
	if err != nil {
		log.Printf("zk-SNARK proof verification failed: %v", err)
		return false
	}

	log.Printf("zk-SNARK proof verification successful for file %s", proof.FileHash)
	return true
}

// SubmitProof submits a storage proof to the network for verification
func (pv *ProofVerifier) SubmitProof(proof *StorageProof) error {
	// Get current validators
	committee := pv.vm.GetCurrentCommittee()
	if committee == nil {
		return fmt.Errorf("no active validator committee")
	}

	// Submit proof to multiple validators for redundancy
	submissionCount := 0
	for _, validator := range committee.Validators {
		if validator.PeerID != pv.node.Node().Host.ID() && validator.IsActive {
			if err := pv.sendProofToValidator(validator.PeerID, proof); err == nil {
				submissionCount++
			}

			if submissionCount >= 3 { // Submit to 3 validators
				break
			}
		}
	}

	if submissionCount == 0 {
		return fmt.Errorf("failed to submit proof to any validator")
	}

	log.Printf("Storage proof for %s submitted to %d validators", proof.FileHash, submissionCount)
	return nil
}

// sendProofToValidator sends a proof to a specific validator for verification
func (pv *ProofVerifier) sendProofToValidator(validatorID peer.ID, proof *StorageProof) error {
	// Create proof request
	requestID := fmt.Sprintf("%s-%d", proof.FileHash, time.Now().UnixNano())
	proofRequest := &ProofRequest{
		Proof:       proof,
		RequestID:   requestID,
		RequesterID: pv.node.Node().Host.ID(),
		Timestamp:   time.Now(),
	}

	// Serialize the proof request
	requestData, err := json.Marshal(proofRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal proof request: %w", err)
	}

	log.Printf("Sending zk-SNARK proof for %s to validator %s (request: %s)",
		proof.FileHash, validatorID, requestID)

	// Send via libp2p stream
	stream, err := pv.node.Node().Host.NewStream(pv.node.Ctx(), validatorID, "/shardspace/proof/1.0.0")
	if err != nil {
		return fmt.Errorf("failed to create stream to validator %s: %w", validatorID, err)
	}
	defer stream.Close()

	// Write proof request
	if _, err := stream.Write(requestData); err != nil {
		return fmt.Errorf("failed to send proof request: %w", err)
	}

	// Read response
	buffer := make([]byte, 1024)
	n, err := stream.Read(buffer)
	if err != nil {
		return fmt.Errorf("failed to read proof response: %w", err)
	}

	var response ProofResponse
	if err := json.Unmarshal(buffer[:n], &response); err != nil {
		return fmt.Errorf("failed to unmarshal proof response: %w", err)
	}

	// Verify response signature
	if err := pv.cryptoMgr.Verify(validatorID, []byte(response.RequestID+response.ProofHash), response.Signature); err != nil {
		return fmt.Errorf("invalid response signature from validator: %w", err)
	}

	if response.IsValid {
		log.Printf("Proof for %s verified successfully by validator %s", proof.FileHash, validatorID)
	} else {
		log.Printf("Proof for %s failed verification by validator %s: %s", proof.FileHash, validatorID, response.Error)
	}

	return nil
}

// registerProofHandler registers the proof verification handler
func (pv *ProofVerifier) registerProofHandler() error {
	// Set stream handler for proof verification requests
	pv.node.Node().Host.SetStreamHandler("/shardspace/proof/1.0.0", pv.handleProofRequest)
	return nil
}

// handleProofRequest handles incoming proof verification requests
func (pv *ProofVerifier) handleProofRequest(stream libstream.Stream) {
	defer stream.Close()

	// Read proof request
	buffer := make([]byte, 4096) // Larger buffer for proof data
	n, err := stream.Read(buffer)
	if err != nil {
		log.Printf("Failed to read proof request: %v", err)
		return
	}

	var proofRequest ProofRequest
	if err := json.Unmarshal(buffer[:n], &proofRequest); err != nil {
		log.Printf("Failed to unmarshal proof request: %v", err)
		return
	}

	log.Printf("Received proof verification request %s for file %s from %s",
		proofRequest.RequestID, proofRequest.Proof.FileHash, proofRequest.RequesterID)

	// Verify the proof
	verificationResult, err := pv.VerifyStorageProof(proofRequest.Proof)
	if err != nil {
		log.Printf("Proof verification error: %v", err)
		return
	}

	// Create and send response
	response := ProofResponse{
		RequestID:  proofRequest.RequestID,
		ProofHash:  proofRequest.Proof.FileHash,
		IsValid:    verificationResult.IsValid,
		VerifiedBy: pv.node.Node().Host.ID(),
		Timestamp:  time.Now(),
		Error:      verificationResult.Error,
	}

	// Sign the response
	signature, err := pv.cryptoMgr.Sign([]byte(response.RequestID + response.ProofHash))
	if err != nil {
		log.Printf("Failed to sign proof response: %v", err)
		return
	}
	response.Signature = signature

	// Send response
	responseData, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal proof response: %v", err)
		return
	}

	if _, err := stream.Write(responseData); err != nil {
		log.Printf("Failed to send proof response: %v", err)
		return
	}

	log.Printf("Sent proof verification response for request %s: valid=%t",
		proofRequest.RequestID, verificationResult.IsValid)
}

// BatchVerifyProofs verifies multiple proofs in a batch (more efficient)
func (pv *ProofVerifier) BatchVerifyProofs(proofs []*StorageProof) []*VerificationResult {
	results := make([]*VerificationResult, len(proofs))

	for i, proof := range proofs {
		result, err := pv.VerifyStorageProof(proof)
		if err != nil {
			result = &VerificationResult{
				Proof:      proof,
				IsValid:    false,
				VerifiedBy: pv.node.Node().Host.ID(),
				Timestamp:  time.Now(),
				Error:      fmt.Sprintf("verification error: %v", err),
			}
		}
		results[i] = result
	}

	return results
}

// GetZKProver returns the underlying zk-SNARK prover instance
func (pv *ProofVerifier) GetZKProver() *zksnark.StorageProofProver {
	return pv.zkProver
}

// IsInitialized checks if the proof verifier is properly initialized
func (pv *ProofVerifier) IsInitialized() bool {
	return pv.initialized
}
