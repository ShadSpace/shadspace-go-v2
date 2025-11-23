package network

import (
	"context"
	"log"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
)

// PeerDiscovery handles peer discovery using DHT and gossip
type PeerDiscovery struct {
	host host.Host
	dht  *dht.IpfsDHT
	node *DecentralizedNode
	mu   sync.RWMutex
}

// NewPeerDiscovery creates a new peer discovery service
func NewPeerDiscovery(host host.Host, dht *dht.IpfsDHT, node *DecentralizedNode) *PeerDiscovery {
	return &PeerDiscovery{
		host: host,
		dht:  dht,
		node: node,
	}
}

// Start begins peer discovery
func (pd *PeerDiscovery) Start() {
	// Start periodic peer discovery
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("Peer Discovery Started")
	pd.discoverPeers()

	for {
		select {
		case <-pd.node.ctx.Done():
			return
		case <-ticker.C:
			pd.discoverPeers()
		}
	}

}

// discoverPeers discovers new peers using the host's peer list as a fallback
func (pd *PeerDiscovery) discoverPeers() {
	// Use DHT routing table to find peers
	routingTable := pd.dht.RoutingTable()
	if routingTable == nil {
		log.Printf("Routing table not ready yet")
	} else {
		log.Printf("Routing table is ready")
	}

	//  Get peers from routing table
	peers := routingTable.ListPeers()

	for _, peerID := range peers {
		if peerID == pd.host.ID() {
			continue
		}

		// Check if we already know this peer
		pd.node.networkView.mu.RLock()
		_, known := pd.node.networkView.peers[peerID]
		pd.node.networkView.mu.RUnlock()

		if known {
			continue
		}

		addrs, err := pd.dht.FindPeer(pd.node.ctx, peerID)
		if err != nil {
			log.Printf("Failed to find addresses for peer %s: %v", peerID, err)
			continue
		}

		if len(addrs.Addrs) == 0 {
			continue
		}

		// Try to connect to discovered peer
		connectCtx, cancel := context.WithTimeout(pd.node.ctx, 10*time.Second)
		defer cancel()

		if err := pd.host.Connect(connectCtx, addrs); err != nil {
			log.Printf("Failed to connect to discovered peer %s: %v", peerID, err)
			continue
		}

		log.Printf("Connected to discovered peer: %s", peerID)
	}

}
