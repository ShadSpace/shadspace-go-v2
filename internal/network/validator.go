package network

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ValidatorCommitte represents the current committee of validators
type ValidatorCommittee struct {
	Validators []*Validator
	StartTime  time.Time
	EndTime    time.Time
	CycleID    uint64
}

// Validator represemts a node selected for validation duties
type Validator struct {
	PeerID         peer.ID
	StakeAmount    *big.Int
	Reputation     float64
	Weight         float64
	IsActive       bool
	LastProven     time.Time
	SuccessCount   uint64
	FailureCount   uint64
	ProofsVerified uint64
	Availability   float64
	ResponseTime   time.Duration
}

// ValidatorManager handles validator selection and management
type ValidatorManager struct {
	node          *DecentralizedNode
	ctx           context.Context
	cancel        context.CancelFunc
	currentCycle  *ValidatorCommittee
	nextCycle     *ValidatorCommittee
	mu            sync.RWMutex
	cycleDuration time.Duration
	minStake      *big.Int
	proofVerifier *ProofVerifier
}

// NewValidatorManager creates a new validator manager
func NewValidatorManager(node *DecentralizedNode) *ValidatorManager {
	ctx, cancel := context.WithCancel(context.Background())

	vm := &ValidatorManager{
		node:          node,
		ctx:           ctx,
		cancel:        cancel,
		cycleDuration: 24 * time.Hour,
		minStake:      big.NewInt(1000), //Minimum stake amount
	}

	// Initialize proof verifier
	proofVerifier, err := NewProofVerifier(node, vm)
	if err != nil {
		log.Fatalf("Failed to initialize proof verifier: %v", err)
	} else {
		vm.proofVerifier = proofVerifier
	}

	return vm
}

// In ValidatorManager.Start()
func (vm *ValidatorManager) Start() error {
	log.Println("Starting validator manager...")

	// Wait for network to stabilize and peers to be discovered
	go vm.delayedValidatorSelection()

	log.Println("Validator manager started successfully")
	return nil
}

func (vm *ValidatorManager) delayedValidatorSelection() {
	// Wait for network to stabilize
	time.Sleep(2 * time.Minute)

	// Retry selection until successful
	for {
		select {
		case <-vm.ctx.Done():
			return
		default:
			if err := vm.performValidationSelection(); err != nil {
				log.Printf("Validator selection failed (will retry in 1 minute): %v", err)
				time.Sleep(1 * time.Minute)
				continue
			}
			return // Success
		}
	}
}

// performValidationSelection selects validators for the next cycle
func (vm *ValidatorManager) performValidationSelection() error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	// Get all eligible peers from network view
	eligiblePeers := vm.getEligiblePeers()
	if len(eligiblePeers) == 0 {
		return fmt.Errorf("no eligible peers for validator selection")
	}

	// Calculate weights for each peer
	vm.calculateWeights(eligiblePeers)

	// Select validators using weighted random selection
	// Dynamic committee size - use available peers but minimum of 3
	committeeSize := len(eligiblePeers)
	if committeeSize > 21 {
		committeeSize = 21
	} else if committeeSize < 3 {
		committeeSize = len(eligiblePeers) // Use whatever we have
	}

	log.Printf("Selecting %d validators from %d eligible peers", committeeSize, len(eligiblePeers))

	// Select validators using weighted random selection
	selectedValidators := vm.selectValidators(eligiblePeers, committeeSize)

	// updated the selectedNodes Tags to include "validator"
	for _, validator := range selectedValidators {
		vm.node.NetworkView().WithWriteLock(func() {
			if peerInfo, exists := vm.node.NetworkView().peers[validator.PeerID]; exists {
				// Check if "validator" tag already exists
				hasValidatorTag := false
				for _, tag := range peerInfo.Info.Tags {
					if tag == "validator" {
						hasValidatorTag = true
						break
					}
				}

				// Add "validator" tag if not already present
				if !hasValidatorTag {
					peerInfo.Info.Tags = append(peerInfo.Info.Tags, "validator")
					log.Printf("Added validator tag to peer: %s", validator.PeerID)
				}
			}
		})
	}

	// Create new committee
	newCommittee := &ValidatorCommittee{
		Validators: selectedValidators,
		StartTime:  time.Now(),
		EndTime:    time.Now().Add(vm.cycleDuration),
		CycleID:    uint64(time.Now().Unix()),
	}

	// Rotate committees
	vm.nextCycle = newCommittee

	// Give some time for network propagation before switching
	go func() {
		time.Sleep(1 * time.Minute)
		vm.mu.Lock()
		vm.currentCycle = vm.nextCycle
		vm.nextCycle = nil
		vm.mu.Unlock()

		log.Printf("New validator committee activated with %d validators", len(selectedValidators))
		vm.broadcastCommitteeUpdate(newCommittee)
	}()
	return nil
}

// getEligiblePeers return all peers eligible for validator selection
func (vm *ValidatorManager) getEligiblePeers() []*Validator {
	var eligiblePeers []*Validator

	// Include self
	selfPeerID := vm.node.Node().Host.ID()
	selfStakeAmount := vm.getStakeAmount(selfPeerID)
	selfReputation := vm.node.GetReputation().GetPeerScore(selfPeerID)

	selfValidator := &Validator{
		PeerID:       selfPeerID,
		StakeAmount:  selfStakeAmount,
		Reputation:   selfReputation,
		IsActive:     true,
		Availability: 1.0,
		ResponseTime: time.Millisecond * 100,
	}
	eligiblePeers = append(eligiblePeers, selfValidator)
	log.Printf("Added self to eligible peers: %s (stake: %s, reputation: %.2f)",
		selfPeerID, selfStakeAmount.String(), selfReputation)

	peers := vm.node.NetworkView().GetPeers()
	for peerID, peerInfo := range peers {
		if !vm.isEligibleForSelection(peerID, peerInfo) {
			continue
		}

		// TODO: Get stake amount (in real implementation, this would come from blockchain)
		stakeAmount := vm.getStakeAmount(peerID)
		fmt.Printf("Peer %s has stake amount: %s\n", peerID, stakeAmount.String())

		// Get reputation score
		reputation := vm.node.GetReputation().GetPeerScore(peerID)
		fmt.Printf("Peer %s has reputation score: %f\n", peerID, reputation)

		validator := &Validator{
			PeerID:       peerID,
			StakeAmount:  stakeAmount,
			Reputation:   reputation,
			IsActive:     true,
			Availability: 1.0,
			ResponseTime: time.Millisecond * 100,
		}

		eligiblePeers = append(eligiblePeers, validator)
	}

	return eligiblePeers
}

// isEligibleForSelection checks if a peer if eligible for validator selection
func (vm *ValidatorManager) isEligibleForSelection(peerID peer.ID, peerInfo *PeerInfo) bool {
	if peerInfo == nil {
		log.Printf("Peer %s has no peer info", peerID)
		return false
	}

	// Check if peer has been seen recently
	if time.Since(peerInfo.LastSeen) > time.Hour {
		log.Printf("Peer %s is too old (last seen: %v)", peerID, peerInfo.LastSeen)
		return false
	}

	// Check minimum reputation
	if vm.node.GetReputation().GetPeerScore(peerID) < 0.3 {
		return false
	}

	// Check if peer meets minimum stake requirements
	stakeAmount := vm.getStakeAmount(peerID)
	if stakeAmount.Cmp(vm.minStake) < 0 {
		return false
	}

	// Check connection status (should be connected)
	if vm.node.Node().Host.Network().Connectedness(peerID).String() != "Connected" {
		log.Printf("node not connected: %s", vm.node.Node().Host.Network().Connectedness(peerID).String())
		return false
	}

	fmt.Printf("Peer %s is eligible for selection\n", peerID)
	return true
}

// // getStakeAmount retrieves the stake amount for a peer
// TODO: In production, this would query the blockchain or staking contract
func (vm *ValidatorManager) getStakeAmount(peerID peer.ID) *big.Int {
	// Mock implementation - in real system, this would query blockchain
	// For now, return a random stake amount for simulation
	randomStake, _ := rand.Int(rand.Reader, big.NewInt(10000))
	return randomStake.Add(randomStake, vm.minStake)
}

// calculateWieghts calculates selection weights for each validator candidate
func (vm *ValidatorManager) calculateWeights(validators []*Validator) {
	for _, validator := range validators {
		// Weight calculation formula: w = (stake_factor * 0.6) + (reputation_factor * 0.4)
		stakeFactor := new(big.Float).SetInt(validator.StakeAmount)
		stakeWeight := new(big.Float).Quo(stakeFactor, new(big.Float).SetInt(big.NewInt(10000)))
		stakeScore, _ := stakeWeight.Float64()

		reputationScore := validator.Reputation
		availabilityScore := validator.Availability

		// Combined weight
		validator.Weight = (stakeScore * 0.4) + (reputationScore * 0.3) + (availabilityScore * 0.3)
	}
}

// selectValidators performs weighted random selection of validators
func (vm *ValidatorManager) selectValidators(candidates []*Validator, count int) []*Validator {
	if len(candidates) <= count {
		return candidates
	}

	// Sort by weight descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Weight > candidates[j].Weight
	})

	// Take top 2/3 by weight, then random selection for the rest
	topCount := (count * 2) / 3
	topValidators := candidates[:topCount]

	// For the remaining slots, use weighted random selection from the rest
	remainingCandidates := candidates[topCount:]
	remainingSlots := count - topCount

	if remainingSlots > 0 && len(remainingCandidates) > 0 {
		selected := vm.weightedRandomSelection(remainingCandidates, remainingSlots)
		topValidators = append(topValidators, selected...)
	}

	return topValidators
}

// weightedRandomSelection performs weighted random selection
func (vm *ValidatorManager) weightedRandomSelection(candidates []*Validator, count int) []*Validator {
	// Calculate total weight
	totalWeight := 0.0
	for _, candidate := range candidates {
		totalWeight += candidate.Weight
	}

	selected := make([]*Validator, 0, count)
	selectedMap := make(map[peer.ID]bool)

	for len(selected) < count && len(candidates) > 0 {
		// Generate random number
		randomValue, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		randomFloat := float64(randomValue.Int64()) / (1 << 62)
		randomFloat *= totalWeight

		// Select candidate based on weighted random
		cumulativeWeight := 0.0
		for _, candidate := range candidates {
			if selectedMap[candidate.PeerID] {
				continue
			}

			cumulativeWeight += candidate.Weight
			if randomFloat <= cumulativeWeight {
				selected = append(selected, candidate)
				selectedMap[candidate.PeerID] = true
				totalWeight -= candidate.Weight
				break
			}
		}
	}

	return selected
}

// GetCurrentCommittee returns the current validator committee
func (vm *ValidatorManager) GetCurrentCommittee() *ValidatorCommittee {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return vm.currentCycle
}

// IsValidator checks if the current node is a validator in the current committee
func (vm *ValidatorManager) IsValidator() bool {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	if vm.currentCycle == nil {
		return false
	}

	nodeID := vm.node.Node().Host.ID()
	for _, validator := range vm.currentCycle.Validators {
		if validator.PeerID == nodeID {
			return true
		}
	}

	return false
}

// GetProofVerifier returns the proof verifier instance
func (vm *ValidatorManager) GetProofVerifier() *ProofVerifier {
	return vm.proofVerifier
}

// UpdateValidatorStats updates validator performance statistics
func (vm *ValidatorManager) UpdateValidatorStats(validatorID peer.ID, proofVerified bool, responseTime time.Duration) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.currentCycle == nil {
		return
	}

	for _, validator := range vm.currentCycle.Validators {
		if validator.PeerID == validatorID {
			if proofVerified {
				validator.SuccessCount++
				validator.ProofsVerified++
			} else {
				validator.FailureCount++
			}

			// Update response time (moving average)
			if validator.ResponseTime == 0 {
				validator.ResponseTime = responseTime
			} else {
				validator.ResponseTime = (validator.ResponseTime*9 + responseTime) / 10
			}

			// Update availability
			totalRequests := validator.SuccessCount + validator.FailureCount
			if totalRequests > 0 {
				validator.Availability = float64(validator.SuccessCount) / float64(totalRequests)
			}

			validator.LastProven = time.Now()
			break
		}
	}
}

func (vm *ValidatorManager) broadcastCommitteeUpdate(committee *ValidatorCommittee) {
	if vm.node == nil {
		log.Printf("Cannot broadcast committee update: node is nil")
		return
	}

	if vm.node.GossipManager() == nil {
		log.Printf("Cannot broadcast committee update: gossip manager not available")
		return
	}

	// Validate committee
	if committee == nil || len(committee.Validators) == 0 {
		log.Printf("Cannot broadcast invalid committee")
		return
	}

	log.Printf("Broadcasting new validator committee with %d members for cycle %d",
		len(committee.Validators), committee.CycleID)

	// Convert validators to network format
	validatorInfos := make([]types.ValidatorInfo, 0, len(committee.Validators))
	for _, validator := range committee.Validators {
		validatorInfos = append(validatorInfos, types.ValidatorInfo{
			PeerID:          validator.PeerID,
			StakeAmount:     validator.StakeAmount.String(),
			Reputation:      validator.Reputation,
			Weight:          validator.Weight,
			IsActive:        validator.IsActive,
			ProofsVerified:  validator.ProofsVerified,
			Availability:    validator.Availability,
			AvgResponseTime: validator.ResponseTime.Milliseconds(),
		})
	}

	// Create validator update message
	validatorUpdate := types.ValidatorUpdateMessage{
		CycleID:    committee.CycleID,
		Validators: validatorInfos,
		StartTime:  committee.StartTime,
		EndTime:    committee.EndTime,
		Signature:  []byte{}, // In production, this would be signed
	}

	// Convert to JSON payload
	payload, err := json.Marshal(validatorUpdate)
	if err != nil {
		log.Printf("Failed to marshal validator update: %v", err)
		return
	}

	// Create gossip message
	gossipMsg := types.GossipMessage{
		Type:      types.GossipTypeValidatorUpdate,
		PeerID:    vm.node.Node().Host.ID(),
		Timestamp: time.Now(),
		Payload:   payload,
	}

	// Broadcast via gossip
	if err := vm.node.GossipManager().PublishMessage(gossipMsg); err != nil {
		log.Printf("Failed to broadcast committee update: %v", err)
	} else {
		log.Printf("Successfully broadcasted new validator committee with %d members",
			len(committee.Validators))

		// Log committee members for debugging
		for i, validator := range committee.Validators {
			log.Printf("Committee member %d: %s (stake: %s, weight: %.4f)",
				i+1,
				validator.PeerID.String()[:8]+"...",
				validator.StakeAmount.String(),
				validator.Weight)
		}
	}
}

// Stop stops the validator manager
func (vm *ValidatorManager) Stop() {
	vm.cancel()
	log.Println("Validator manager stopped")
}
