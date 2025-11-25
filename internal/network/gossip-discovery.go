package network

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

type GossipManager struct {
	host   host.Host
	pubsub *pubsub.PubSub
	node   *DecentralizedNode
	topic  *pubsub.Topic
	sub    *pubsub.Subscription
	mu     sync.RWMutex
}

// NewGossipManager creates a new gossip manager
func NewGossipManager(host host.Host, pubsub *pubsub.PubSub, node *DecentralizedNode) *GossipManager {
	return &GossipManager{
		host:   host,
		pubsub: pubsub,
		node:   node,
	}
}

// Start begins the gossip protocal
func (gm *GossipManager) Start() error {
	// Hoin the gossip topic
	topic, err := gm.pubsub.Join("shardspace-network-v1")
	if err != nil {
		return fmt.Errorf("failed to join gossip topic: %w", err)
	}

	gm.topic = topic

	// Subscribe to the topic
	sub, err := topic.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic: %w", err)
	}
	gm.sub = sub

	// Start reading messages
	go gm.readMessages()

	// Start broadcasting node info periodically
	go gm.broadcastNodeInfo()

	log.Println("Gossip manager started")
	return nil
}

// readMessages reads incoming gossip messages
func (gm *GossipManager) readMessages() {
	for {
		msg, err := gm.sub.Next(gm.node.ctx)
		if err != nil {
			if gm.node.ctx.Err() != nil {
				return // Context canceled
			}
			log.Printf("Error reading gossip message: %v", err)
			continue
		}

		if msg.ReceivedFrom == gm.host.ID() {
			continue // Skip our own messages
		}

		go gm.handleGossipMessage(msg)
	}
}

// handleGossipMessage processes incoming gossip messages
func (gm *GossipManager) handleGossipMessage(msg *pubsub.Message) {
	var gossipMsg types.GossipMessage
	if err := json.Unmarshal(msg.Data, &gossipMsg); err != nil {
		log.Printf("Failed to unmarshal gossip message: %v", err)
		return
	}

	switch gossipMsg.Type {
	case types.GossipTypeNodeInfo:
		gm.handleNodeInfoMessage(gossipMsg)
	case types.GossipTypeFileAnnounce:
		gm.handleFileAnnounceMessage(gossipMsg)
	case types.GossipTypeValidatorUpdate:
		gm.handleValidatorUpdateMessage(gossipMsg)
		// case types.GossipTypeReputationUpdate:
		// 	gm.handleReputationMessage(gossipMsg)
	}
}

func (gm *GossipManager) handleNodeInfoMessage(msg types.GossipMessage) {
	var farmerInfo types.FarmerInfo
	if err := json.Unmarshal(msg.Payload, &farmerInfo); err != nil {
		log.Printf("Failed to unmarshal farmer info: %v", err)
		return
	}

	gm.node.networkView.mu.Lock()
	defer gm.node.networkView.mu.Unlock()

	// Update peer info
	if _, exists := gm.node.networkView.peers[farmerInfo.PeerID]; !exists {
		gm.node.networkView.peers[farmerInfo.PeerID] = &PeerInfo{
			Info:        &farmerInfo,
			LastSeen:    time.Now(),
			Reliability: 1.0,
			Distance:    1,
		}
		log.Printf("Discovered new peer: %s", farmerInfo.PeerID)
	} else {
		peerInfo := gm.node.networkView.peers[farmerInfo.PeerID]
		peerInfo.Info = &farmerInfo
		peerInfo.LastSeen = time.Now()
	}

	// Update reputation system
	gm.node.reputation.UpdatePeer(farmerInfo.PeerID, 0.1)

}

func (gm *GossipManager) handleFileAnnounceMessage(msg types.GossipMessage) {
	var fileAnnounce types.FileAnnounceMessage
	if err := json.Unmarshal(msg.Payload, &fileAnnounce); err != nil {
		log.Printf("Failed to unmarshal file announcement: %v", err)
		return
	}

	gm.node.networkView.mu.Lock()
	defer gm.node.networkView.mu.Unlock()

	// Update file index in network view
	for _, location := range fileAnnounce.Locations {
		// Add or update the file location
		if _, exists := gm.node.networkView.fileIndex[location.FileHash]; !exists {
			gm.node.networkView.fileIndex[location.FileHash] = make([]peer.ID, 0)
		}

		// Add the peer to the file index if not already present
		existingPeers := gm.node.networkView.fileIndex[location.FileHash]
		for _, peerID := range location.PeerIDs {
			found := false
			for _, existingPeer := range existingPeers {
				if existingPeer == peerID {
					found = true
					break
				}
			}
			if !found {
				existingPeers = append(existingPeers, peerID)
			}
		}
		gm.node.networkView.fileIndex[location.FileHash] = existingPeers
	}

	log.Printf("Updated file index for %s: stored on %d peers",
		fileAnnounce.Locations[0].FileHash,
		len(gm.node.networkView.fileIndex[fileAnnounce.Locations[0].FileHash]))
}

// handleValidatorUpdateMessage processes validator committee updates
func (gm *GossipManager) handleValidatorUpdateMessage(msg types.GossipMessage) {
	var validatorUpdate types.ValidatorUpdateMessage
	if err := json.Unmarshal(msg.Payload, &validatorUpdate); err != nil {
		log.Printf("Failed to unmarshal validator update: %v", err)
		return
	}

	log.Printf("Received validator committee update for cycle %d with %d validators",
		validatorUpdate.CycleID, len(validatorUpdate.Validators))

	// In a full implementation, you would:
	// 1. Verify the signature
	// 2. Validate the committee
	// 3. Update local validator state
	// 4. Trigger consensus layer updates

	// For now, just log the update
	for i, validator := range validatorUpdate.Validators {
		log.Printf("Remote committee member %d: %s", i+1, validator.PeerID.String()[:8]+"...")
	}
}

func (gm *GossipManager) handleFileDeleteMessage(msg types.GossipMessage) {
	var deleteMsg types.FileDeleteMessage
	if err := json.Unmarshal(msg.Payload, &deleteMsg); err != nil {
		log.Printf("Failed to unmarshal file deletion message: %v", err)
		return
	}

	gm.node.networkView.mu.Lock()
	defer gm.node.networkView.mu.Unlock()

	// Remove file from network view
	if peers, exists := gm.node.networkView.fileIndex[deleteMsg.FileHash]; exists {
		// Remove this peer from the file's peer list
		updatedPeers := make([]peer.ID, 0)
		for _, peerID := range peers {
			if peerID != deleteMsg.PeerID {
				updatedPeers = append(updatedPeers, peerID)
			}
		}

		if len(updatedPeers) == 0 {
			delete(gm.node.networkView.fileIndex, deleteMsg.FileHash)
			log.Printf("Removed file %s from network view (no more peers)", deleteMsg.FileHash)
		} else {
			gm.node.networkView.fileIndex[deleteMsg.FileHash] = updatedPeers
			log.Printf("Updated file %s in network view: now on %d peers", deleteMsg.FileHash, len(updatedPeers))
		}
	}
}

// func (gm *GossipManager) handleReputationMessage(msg types.GossipMessage) {
// 	var repMsg types.ReputationMessage
// 	if err := json.Unmarshal(msg.Payload, &repMsg); err != nil {
// 		log.Printf("Failed to unmarshal reputation message: %v", err)
// 		return
// 	}

// 	// Update reputation based on network feedback
// 	gm.node.reputation.UpdatePeer(repMsg.PeerID, repMsg.ScoreChange, repMsg.Reason)
// }

// broadcastNodeInfo broadcasts node information to the network
func (gm *GossipManager) broadcastNodeInfo() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-gm.node.ctx.Done():
			return
		case <-ticker.C:
			gm.node.mu.RLock()
			farmerInfo := gm.node.farmerInfo
			gm.node.mu.RUnlock()

			// update storage usage
			stats := gm.node.storage.GetStats()
			farmerInfo.UsedStorage = stats["used_bytes"].(uint64)
			farmerInfo.LastSeen = time.Now()

			msg := types.GossipMessage{
				Type:      types.GossipTypeNodeInfo,
				PeerID:    gm.host.ID(),
				Timestamp: time.Now(),
				Payload:   farmerInfo.ToJSON(),
			}

			if err := gm.PublishMessage(msg); err != nil {
				log.Printf("Failed to broadcast node info: %v", err)
			}

		}
	}
}

// PublishMessage publishes a gossip message
func (gm *GossipManager) PublishMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Defensive check: ensure topic is initialized
	if gm.topic == nil {
		return fmt.Errorf("gossip topic not initialized")
	}

	return gm.topic.Publish(gm.node.ctx, data)
}
