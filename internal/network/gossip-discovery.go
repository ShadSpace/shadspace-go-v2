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
		// case types.GossipTypeFileAnnounce:
		// 	gm.handleFileAnnounceMessage(gossipMsg)
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

			if err := gm.publishMessage(msg); err != nil {
				log.Printf("Failed to broadcast node info: %v", err)
			}

		}
	}
}

// publishMessage publishes a gossip message
func (gm *GossipManager) publishMessage(msg types.GossipMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return gm.topic.Publish(gm.node.ctx, data)
}
