package network

import (
	"log"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
)

type ReputationSystem struct {
	node *DecentralizedNode
	mu   sync.RWMutex
}

// NewReputationSystem creates a new reputation system
func NewReputationSystem(node *DecentralizedNode) *ReputationSystem {
	return &ReputationSystem{
		node: node,
	}
}

// UpdatePeer updates a peer's reputation score
func (rs *ReputationSystem) UpdatePeer(peerID peer.ID, scoreChange float64) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	currentScore := rs.node.networkView.reputation[peerID]
	newScore := currentScore + scoreChange

	// Clamp between 0 and 1
	if newScore < 0 {
		newScore = 0
	} else if newScore > 1 {
		newScore = 1
	}

	rs.node.networkView.reputation[peerID] = newScore
	log.Printf("Updated reputation for %s: %.2f -> %.2f (change: %+.2f)",
		peerID, currentScore, newScore, scoreChange)
}

// GetPeerScore returns a peer's reputation score
func (rs *ReputationSystem) GetPeerScore(peerID peer.ID) float64 {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if score, exists := rs.node.networkView.reputation[peerID]; exists {
		return score
	}

	return 0.5 //Default score for unkown peers
}
