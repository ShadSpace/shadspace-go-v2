package network

import (
	"context"
	"log"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerDiscovery handles peer discovery using DHT and gossip
type PeerDiscovery struct {
	host       host.Host
	dht        *dht.IpfsDHT
	node       *DecentralizedNode
	mu         sync.RWMutex
	retryCount map[peer.ID]int
	maxRetries int
}

// NewPeerDiscovery creates a new peer discovery service
func NewPeerDiscovery(host host.Host, dht *dht.IpfsDHT, node *DecentralizedNode) *PeerDiscovery {
	return &PeerDiscovery{
		host:       host,
		dht:        dht,
		node:       node,
		retryCount: make(map[peer.ID]int),
		maxRetries: 3,
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

func (pd *PeerDiscovery) discoverPeers() {
	log.Printf("Starting enhanced peer discovery...")

	// Method 1: Use DHT routing table
	pd.discoverFromDHT()

	// Method 2: Use peerstore (already connected peers)
	pd.discoverFromPeerstore()

	// Method 3: Use DHT provider discovery
	// go pd.discoverFromProviders()
}

// discoverFromDHT discovers peers from DHT routing table
func (pd *PeerDiscovery) discoverFromDHT() {
	routingTable := pd.dht.RoutingTable()
	if routingTable == nil {
		log.Printf("DHT routing table not ready yet")
		return
	}

	peers := routingTable.ListPeers()
	log.Printf("DHT routing table has %d peers", len(peers))

	for _, peerID := range peers {
		pd.tryConnectToPeer(peerID, "DHT routing table")
	}
}

// discoverFromPeerstore discovers peers from host's peerstore
func (pd *PeerDiscovery) discoverFromPeerstore() {
	peers := pd.host.Peerstore().Peers()
	log.Printf("Peerstore has %d peers", len(peers))

	connectedCount := 0
	for _, peerID := range peers {
		if peerID == pd.host.ID() || peerID == "" {
			continue
		}

		// Check if already connected
		if pd.host.Network().Connectedness(peerID) == network.Connected {
			connectedCount++
			continue
		}

		// Only try to connect if we have addresses for this peer
		addrs := pd.host.Peerstore().Addrs(peerID)
		if len(addrs) > 0 {
			pd.tryConnectToPeer(peerID, "peerstore")
		}
	}

	log.Printf("Already connected to %d peers from peerstore", connectedCount)
}

// discoverFromProviders discovers peers via DHT provider records
// func (pd *PeerDiscovery) discoverFromProviders() {
// 	// Look for providers of our protocol
// 	protocolID := protocol.ID(pd.node.node.Config.ProtocolID)

// 	peerCh, err := pd.dht.FindProviders(pd.node.ctx, string(protocolID))
// 	if err != nil {
// 		log.Printf("Error finding providers: %v", err)
// 		return
// 	}

// 	providerCount := 0
// 	for peerInfo := range peerCh {
// 		if peerInfo.ID == pd.host.ID() || len(peerInfo.Addrs) == 0 {
// 			continue
// 		}

// 		providerCount++
// 		pd.tryConnectToPeer(peerInfo.ID, "DHT providers")
// 	}

// 	log.Printf("Found %d providers for protocol %s", providerCount, protocolID)
// }

// tryConnectToPeer attempts to connect to a peer with proper error handling and retry logic
func (pd *PeerDiscovery) tryConnectToPeer(peerID peer.ID, source string) {
	// Check if we already know this peer and are connected
	pd.node.networkView.mu.RLock()
	_, known := pd.node.networkView.peers[peerID]
	pd.node.networkView.mu.RUnlock()

	if known && pd.host.Network().Connectedness(peerID) == network.Connected {
		return // Already connected and known
	}

	// Check retry count
	pd.mu.Lock()
	retries := pd.retryCount[peerID]
	if retries >= pd.maxRetries {
		pd.mu.Unlock()
		log.Printf("Skipping peer %s - exceeded max retry attempts (%d)", peerID, pd.maxRetries)
		return
	}
	pd.mu.Unlock()

	// Get addresses for the peer
	var addrs peer.AddrInfo
	var err error

	// Try DHT first
	addrs, err = pd.dht.FindPeer(pd.node.ctx, peerID)
	if err != nil || len(addrs.Addrs) == 0 {
		// Fall back to peerstore
		addrs = peer.AddrInfo{
			ID:    peerID,
			Addrs: pd.host.Peerstore().Addrs(peerID),
		}
	}

	if len(addrs.Addrs) == 0 {
		log.Printf("No addresses found for peer %s from %s", peerID, source)
		return
	}

	// Try to connect to discovered peer
	connectCtx, cancel := context.WithTimeout(pd.node.ctx, 10*time.Second)
	defer cancel()

	log.Printf("Attempting to connect to peer %s from %s (attempt %d/%d)",
		peerID, source, retries+1, pd.maxRetries)

	if err := pd.host.Connect(connectCtx, addrs); err != nil {
		log.Printf("Failed to connect to peer %s: %v", peerID, err)

		// Increment retry count
		pd.mu.Lock()
		pd.retryCount[peerID] = retries + 1
		pd.mu.Unlock()

		return
	}

	log.Printf("✅ Successfully connected to peer: %s from %s", peerID, source)

	// Reset retry count on successful connection
	pd.mu.Lock()
	delete(pd.retryCount, peerID)
	pd.mu.Unlock()

	// Update network view safely
	pd.updateNetworkView(peerID)
}

// updateNetworkView safely updates the network view for a new peer
func (pd *PeerDiscovery) updateNetworkView(peerID peer.ID) {
	pd.node.networkView.mu.Lock()
	defer pd.node.networkView.mu.Unlock()

	if _, exists := pd.node.networkView.peers[peerID]; !exists {
		pd.node.networkView.peers[peerID] = &PeerInfo{
			Info:        nil, // Will be populated via gossip
			LastSeen:    time.Now(),
			Reliability: 1.0,
			Distance:    1,
		}
		log.Printf("Added new peer to network view: %s", peerID)
	}

	// Update reputation for successful connection
	pd.node.reputation.UpdatePeer(peerID, 0.1)
}

// GetConnectedPeers returns list of currently connected peers
func (pd *PeerDiscovery) GetConnectedPeers() []peer.ID {
	conns := pd.host.Network().Conns()
	peers := make([]peer.ID, 0, len(conns))

	for _, conn := range conns {
		peers = append(peers, conn.RemotePeer())
	}

	return peers
}

// IsPeerConnected checks if a peer is currently connected
func (pd *PeerDiscovery) IsPeerConnected(peerID peer.ID) bool {
	return pd.host.Network().Connectedness(peerID) == network.Connected
}

// CleanupRetryHistory removes old entries from retry count map
func (pd *PeerDiscovery) CleanupRetryHistory() {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	// Remove entries older than 1 hour (simplified - in real implementation, track timestamps)
	// For now, just limit the size
	if len(pd.retryCount) > 1000 {
		pd.retryCount = make(map[peer.ID]int)
		log.Println("Cleared retry history due to size limit")
	}
}
