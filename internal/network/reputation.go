package network

import (
	"log"
	"sync"
	"time"

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

// MonitorPeers periodically updates
func (rs *ReputationSystem) MonitorPeers() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	log.Println("Reputation system monitor started")

	for {
		select {
		case <-rs.node.ctx.Done():
			return
		case <-ticker.C:
			rs.performReputationUpdate()
		}
	}
}

// performReputationUpdate updates reputation based on peer activity
func (rs *ReputationSystem) performReputationUpdate() {
	rs.node.networkView.mu.RLock()
	defer rs.node.networkView.mu.RUnlock()

	currentTime := time.Now()
	staleThreshold := currentTime.Add(-30 * time.Minute)
	inactiveThreshold := currentTime.Add(-2 * time.Hour)

	for peerID, peerInfo := range rs.node.networkView.peers {
		timeSinceLastSeen := currentTime.Sub(peerInfo.LastSeen)

		// Apply reputation decay for stale peers
		if peerInfo.LastSeen.Before(staleThreshold) {
			decayAmount := -0.05
			if peerInfo.LastSeen.Before(inactiveThreshold) {
				decayAmount = -0.1
			}
			rs.UpdatePeer(peerID, decayAmount)
			log.Printf("Applied reputation decay %.2f to inactive peer %s", decayAmount, peerID)
		}

		// Boost reputation for reliable, active peers
		if timeSinceLastSeen < 10*time.Minute && peerInfo.Reliability > 0.8 {
			rs.UpdatePeer(peerID, 0.02)
		}
	}
}

func (rs *ReputationSystem) GetBestPeers(minScore float64, maxPeers int) []peer.ID {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	type peerScore struct {
		peerID peer.ID
		score  float64
	}

	var scoredPeers []peerScore

	// Collect peers that meet minimum score requirement
	for peerID, score := range rs.node.networkView.reputation {
		if score >= minScore {
			scoredPeers = append(scoredPeers, peerScore{peerID, score})
		}
	}

	//  Sort by score (decending)
	for i := 0; i < len(scoredPeers); i++ {
		for j := i + 1; j < len(scoredPeers); j++ {
			if scoredPeers[j].score > scoredPeers[i].score {
				scoredPeers[i], scoredPeers[j] = scoredPeers[j], scoredPeers[i]
			}
		}
	}

	// return top peers
	result := make([]peer.ID, 0, maxPeers)
	for i := 0; i < len(scoredPeers) && i < maxPeers; i++ {
		result = append(result, scoredPeers[i].peerID)
	}

	return result
}

// RecordSuccessfulInteraction records a positive interaction with a peer
func (rs *ReputationSystem) RecordSuccessfulInteraction(peerID peer.ID) {
	rs.UpdatePeer(peerID, 0.05)

	// Also update reliability in peer info
	rs.node.networkView.mu.Lock()
	if peerInfo, exists := rs.node.networkView.peers[peerID]; exists {
		peerInfo.Reliability = min(1.0, peerInfo.Reliability+0.05)
	}
	rs.node.networkView.mu.Unlock()
}

// RecordFailedInteraction records a negative interaction with a peer
func (rs *ReputationSystem) RecordFailedInteraction(peerID peer.ID) {
	rs.UpdatePeer(peerID, -0.1)

	// Also update reliability in peer info
	rs.node.networkView.mu.Lock()
	if peerInfo, exists := rs.node.networkView.peers[peerID]; exists {
		peerInfo.Reliability = max(0.0, peerInfo.Reliability-0.1)
	}
	rs.node.networkView.mu.Unlock()
}

// GetPeerReliability returns the reliability score of a peer
func (rs *ReputationSystem) GetPeerReliability(peerID peer.ID) float64 {
	rs.node.networkView.mu.RLock()
	defer rs.node.networkView.mu.RUnlock()

	if peerInfo, exists := rs.node.networkView.peers[peerID]; exists {
		return peerInfo.Reliability
	}
	return 0.5 // Default reliability for unknown peers
}

// Helper functions
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
