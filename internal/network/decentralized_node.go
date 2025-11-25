package network

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
	"github.com/multiformats/go-multiaddr"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type DecentralizedNode struct {
	node         *Node
	ctx          context.Context
	cancel       context.CancelFunc
	storage      *storage.Engine
	shardManager *storage.ShardManager

	// Dentralized components
	dht        *dht.IpfsDHT
	pubsub     *pubsub.PubSub
	gossip     *GossipManager
	discovery  *PeerDiscovery
	reputation *ReputationSystem

	// Consensus components
	posManager *PoSManager

	// Distributed state
	farmerInfo  *types.FarmerInfo
	networkView *NetworkView
	mu          sync.RWMutex

	storageOps  *StorageOperations
	fileManager *storage.FileManager
}

// NetworkView represents the node's view of the network
type NetworkView struct {
	peers       map[peer.ID]*PeerInfo
	fileIndex   map[string][]peer.ID
	reputation  map[peer.ID]float64
	lastUpdated time.Time
	mu          sync.RWMutex
}

type PeerInfo struct {
	Info        *types.FarmerInfo
	LastSeen    time.Time
	Reliability float64
	Distance    int
}

func NewDecentralizedNode(ctx context.Context, config NodeConfig) (*DecentralizedNode, error) {
	nodeCtx, cancel := context.WithCancel(ctx)

	// Create base node
	baseNode, err := NewNode(nodeCtx, config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create base node: %w", err)
	}

	// Create storage director stracture
	storageBaseDir := "./data"
	nodeStorageDir := filepath.Join(storageBaseDir, "nodes", baseNode.Host.ID().String())
	fileStorageDir := nodeStorageDir

	if err := os.MkdirAll(nodeStorageDir, 0755); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create node storage directory: %w", err)
	}
	if err := os.MkdirAll(fileStorageDir, 0755); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create file storage directory: %w", err)
	}

	// Initialize DHT for distribution hash table
	dhtInstance, err := dht.New(nodeCtx, baseNode.Host, dht.Mode(dht.ModeAuto))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	ps, err := pubsub.NewGossipSub(nodeCtx, baseNode.Host)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	// Initialize storage
	storageEngine, err := storage.NewEngine(nodeStorageDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create storage engine: %w", err)
	}

	// Create farmer info
	farmerInfo := &types.FarmerInfo{
		PeerID:          baseNode.Host.ID(),
		NodeName:        utils.GenerateNodeName(baseNode.Host.ID()),
		Version:         "0.0.1",
		Addresses:       utils.GetNodeAddresses(baseNode.Host),
		StorageCapacity: 10 * 1024 * 1024 * 1024, // 10GB
		UsedStorage:     0,
		Reliability:     1.0,
		Location:        "unknown",
		IsActive:        true,
		Tags:            []string{"farmer", "decentralized"},
		StartTime:       time.Now(),
		LastSeen:        time.Now(),
	}

	//  Create decentralised node
	dn := &DecentralizedNode{
		node:         baseNode,
		ctx:          nodeCtx,
		cancel:       cancel,
		storage:      storageEngine,
		shardManager: storage.NewShardManager(),
		dht:          dhtInstance,
		pubsub:       ps,
		farmerInfo:   farmerInfo,
		networkView: &NetworkView{
			peers:      make(map[peer.ID]*PeerInfo),
			fileIndex:  make(map[string][]peer.ID),
			reputation: make(map[peer.ID]float64),
		},
	}

	fileManager, err := storage.NewFileManager(fileStorageDir)
	if err != nil {
		log.Fatal(err)
	}

	storageOps := NewStorageOperations(dn, fileManager)

	dn.fileManager = fileManager
	dn.storageOps = storageOps

	dn.gossip = NewGossipManager(baseNode.Host, ps, dn)
	dn.discovery = NewPeerDiscovery(baseNode.Host, dhtInstance, dn)
	dn.reputation = NewReputationSystem(dn)
	dn.posManager = NewPoSManager(dn)

	// Set the decentralized node as the file store for protocol handlers
	baseNode.SetFileStore(dn)

	// Bootstrap the node
	if err := dn.bootstrap(); err != nil {
		log.Printf("Bootstrap warning: %v", err)
	}

	baseNode.Host.Network().Notify(&network.NotifyBundle{
		ConnectedF:    dn.onPeerConnected,
		DisconnectedF: dn.onPeerDisconnected,
	})

	// Start background processes
	go dn.maintainNetworkState()
	// Start gossip synchronously to ensure the topic and subscription are initialized
	if err := dn.gossip.Start(); err != nil {
		log.Printf("Warning: failed to start gossip manager: %v", err)
	}

	// Start PoSManager (this starts all consensus components)
	if err := dn.posManager.Start(); err != nil {
		log.Printf("Warning: failed to start PoS manager: %v", err)
	} else {
		log.Printf("✅ Consensus system started successfully")
	}

	go dn.discovery.Start()
	go dn.reputation.MonitorPeers()
	go storageOps.MonitorStorageOperations()

	// Populate network view with locally stored files so other peers can discover them
	// and so this node advertises its holdings even if gossip hasn't propagated yet.
	localFiles := dn.fileManager.ListFiles()
	if len(localFiles) > 0 {
		dn.networkView.mu.Lock()
		for _, meta := range localFiles {
			if _, exists := dn.networkView.fileIndex[meta.FileHash]; !exists {
				dn.networkView.fileIndex[meta.FileHash] = make([]peer.ID, 0)
			}
			// Add this node as a storing peer for the file
			dn.networkView.fileIndex[meta.FileHash] = append(dn.networkView.fileIndex[meta.FileHash], baseNode.Host.ID())
		}
		dn.networkView.mu.Unlock()

		// Announce local files via gossip so peers learn about them
		go func() {
			for _, meta := range localFiles {
				storageOps.announceFileToNetwork(meta.FileHash, []peer.ID{baseNode.Host.ID()})
			}
		}()
	}

	log.Printf("Decentralized node initialized with ID: %s", baseNode.Host.ID())
	return dn, nil
}

// GetFileMetadata implements FileStore interface
func (dn *DecentralizedNode) GetFileMetadata(fileHash string) (*storage.FileMetadata, error) {
	return dn.fileManager.GetFileInfo(fileHash)
}

// GetShard implements FileStore interface
func (dn *DecentralizedNode) GetShard(fileHash string, shardIndex int) ([]byte, error) {
	shardID := fmt.Sprintf("%s_shard_%d", fileHash, shardIndex)
	return dn.storage.RetrieveChunk(shardID)
}

// GetFileStats implements FileStore interface
func (dn *DecentralizedNode) GetFileStats() map[string]interface{} {
	return dn.fileManager.GetFileStats()
}

// GetShardManager returns the shard manager instance
func (dn *DecentralizedNode) GetShardManager() *storage.ShardManager {
	return dn.shardManager
}

// GetCodec returns the codec instance for stream communication
func (dn *DecentralizedNode) GetCodec() *codec {
	return dn.node.GetCodec()
}

// DebugNetworkView prints debug information about the network view
func (dn *DecentralizedNode) DebugNetworkView() {
	dn.networkView.mu.RLock()
	defer dn.networkView.mu.RUnlock()

	log.Printf("=== Network View Debug ===")
	log.Printf("Total peers: %d", len(dn.networkView.peers))
	log.Printf("Total files in index: %d", len(dn.networkView.fileIndex))

	for fileHash, peers := range dn.networkView.fileIndex {
		log.Printf("File %s: stored on %d peers %v", fileHash, len(peers), peers)
	}
	log.Printf("=== End Network View Debug ===")
}

// GetPeerInfo returns information about a specific peer
func (dn *DecentralizedNode) GetPeerInfo(peerID peer.ID) (*PeerInfo, bool) {
	dn.networkView.mu.RLock()
	defer dn.networkView.mu.RUnlock()

	info, exists := dn.networkView.peers[peerID]
	return info, exists
}

// UpdateFileIndex updates the file index with new peer information
func (dn *DecentralizedNode) UpdateFileIndex(fileHash string, peerID peer.ID) {
	dn.networkView.mu.Lock()
	defer dn.networkView.mu.Unlock()

	if _, exists := dn.networkView.fileIndex[fileHash]; !exists {
		dn.networkView.fileIndex[fileHash] = make([]peer.ID, 0)
	}

	// Check if peer is already in the list
	peers := dn.networkView.fileIndex[fileHash]
	for _, existingPeer := range peers {
		if existingPeer == peerID {
			return // Peer already exists
		}
	}

	// Add peer to the list
	dn.networkView.fileIndex[fileHash] = append(peers, peerID)
	log.Printf("Updated file index: file %s now stored on %d peers", fileHash, len(dn.networkView.fileIndex[fileHash]))
}

// RemoveFileFromIndex removes a file from the network view
func (dn *DecentralizedNode) RemoveFileFromIndex(fileHash string, peerID peer.ID) {
	dn.networkView.mu.Lock()
	defer dn.networkView.mu.Unlock()

	if peers, exists := dn.networkView.fileIndex[fileHash]; exists {
		updatedPeers := make([]peer.ID, 0)
		for _, p := range peers {
			if p != peerID {
				updatedPeers = append(updatedPeers, p)
			}
		}

		if len(updatedPeers) == 0 {
			delete(dn.networkView.fileIndex, fileHash)
			log.Printf("Removed file %s from network view (no more peers)", fileHash)
		} else {
			dn.networkView.fileIndex[fileHash] = updatedPeers
			log.Printf("Updated file %s in network view: now on %d peers", fileHash, len(updatedPeers))
		}
	}
}

// bootstrap initializes the decentralized node
func (dn *DecentralizedNode) bootstrap() error {
	if err := dn.dht.Bootstrap(dn.ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	// Connect to bootstrap peers
	for _, peerAddr := range dn.node.Config.BootstrapPeers {
		ma, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			return fmt.Errorf("failed to bootstrap DHT: %w", err)
		}

		addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("Failed to parse peer info from %s: %v", peerAddr, err)
			continue
		}

		connectCtx, cancel := context.WithTimeout(dn.ctx, 10*time.Second)
		defer cancel()

		if err := dn.node.Host.Connect(connectCtx, *addrInfo); err != nil {
			log.Printf("Failed to connect to bootstrap peer %s: %v", peerAddr, err)
			continue
		}

		log.Printf("Connected to bootstrap peer: %s", addrInfo.ID)
	}

	return nil
}

// maintainNetworkState maintains the network view and cleans up stale peers
func (dn *DecentralizedNode) maintainNetworkState() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-dn.ctx.Done():
			return
		case <-ticker.C:
			dn.cleanupStalePeers()
			dn.updateNetworkMetrics()
			dn.rebalanceStorage()
		}
	}
}

// cleanupStalePeers removes peers that haven't been seen recently
func (dn *DecentralizedNode) cleanupStalePeers() {
	dn.networkView.mu.Lock()
	defer dn.networkView.mu.Unlock()

	staleThreshold := time.Now().Add(-10 * time.Minute)
	for peerID, peerInfo := range dn.networkView.peers {
		if peerInfo.LastSeen.Before(staleThreshold) {
			delete(dn.networkView.peers, peerID)
			delete(dn.networkView.reputation, peerID)

			// Remove from file index
			for fileHash, peers := range dn.networkView.fileIndex {
				updatedPeers := make([]peer.ID, 0)
				for _, p := range peers {
					if p != peerID {
						updatedPeers = append(updatedPeers, p)
					}
				}
				dn.networkView.fileIndex[fileHash] = updatedPeers
			}

			log.Printf("Removed stale peer: %s", peerID)
		}
	}
}

// updateNetworkMetrics updates network statistics
func (dn *DecentralizedNode) updateNetworkMetrics() {
	dn.networkView.mu.Lock()
	defer dn.networkView.mu.Unlock()

	dn.networkView.lastUpdated = time.Now()

	// Get DHT metrics
	routingTable := dn.dht.RoutingTable()
	var dhtPeerCount int
	if routingTable != nil {
		dhtPeerCount = routingTable.Size()
	}

	log.Printf("Network view updated: %d peers known, %d in DHT routing table",
		len(dn.networkView.peers), dhtPeerCount)
}

// rebalanceStorage checks if files need to be replicated
func (dn *DecentralizedNode) rebalanceStorage() {
	// Check file replication levels and trigger rebalancing if needed
	dn.networkView.mu.RLock()
	defer dn.networkView.mu.RUnlock()

	for fileHash, peers := range dn.networkView.fileIndex {
		if len(peers) < 3 { // Minimum replication factor
			log.Printf("File %s has low replication (%d peers), triggering replication", fileHash, len(peers))
			// In a real implementation, you'd trigger replication here
		}
	}
}

func (dn *DecentralizedNode) onPeerConnected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()

	dn.networkView.mu.Lock()
	defer dn.networkView.mu.Unlock()

	// Check if we already have this peer to avoid duplicates
	if _, exists := dn.networkView.peers[peerID]; !exists {
		log.Printf("🔗 CONNECTED to peer: %s", peerID)
		dn.networkView.peers[peerID] = &PeerInfo{
			Info:        nil, // Will be populated via gossip
			LastSeen:    time.Now(),
			Reliability: 1.0,
			Distance:    1,
		}
	}

	// Small reputation boost for successful connection
	dn.reputation.UpdatePeer(peerID, 0.02)
}

// onPeerDisconnected handles peer disconnections
func (dn *DecentralizedNode) onPeerDisconnected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	dn.networkView.mu.Lock()
	defer dn.networkView.mu.Unlock()

	log.Printf("🔌 DISCONNECTED from peer: %s", peerID)

	// Small reputation penalty for disconnection (remove duplicate line)
	dn.reputation.UpdatePeer(peerID, -0.01)

	// Note: We don't remove the peer from network view immediately
	// The cleanupStalePeers method will handle stale peers
}

// WithReadLock executes a function with read lock held
func (nv *NetworkView) WithReadLock(f func()) {
	nv.mu.RLock()
	defer nv.mu.RUnlock()
	f()
}

// WithWriteLock executes a function with write lock held
func (nv *NetworkView) WithWriteLock(f func()) {
	nv.mu.Lock()
	defer nv.mu.Unlock()
	f()
}

// GetPeers returns a copy of the peers map (thread-safe)
func (nv *NetworkView) GetPeers() map[peer.ID]*PeerInfo {
	nv.mu.RLock()
	defer nv.mu.RUnlock()

	// Return a copy to avoid external modification
	peersCopy := make(map[peer.ID]*PeerInfo)
	for k, v := range nv.peers {
		peersCopy[k] = v
	}
	return peersCopy
}

// GetHostID returns the node's host ID
func (dn *DecentralizedNode) GetHostID() string {
	return dn.node.Host.ID().String()
}

// GetPeerID returns the node's peer ID
func (dn *DecentralizedNode) GetPeerID() peer.ID {
	return dn.node.Host.ID()
}

func (dn *DecentralizedNode) Node() *Node {
	return dn.node
}

// GetStorageOperations returns the storage operations instance
func (dn *DecentralizedNode) GetStorageOperations() *StorageOperations {
	return dn.storageOps
}

// GetFileManager returns the file manager instance
func (dn *DecentralizedNode) GetFileManager() *storage.FileManager {
	return dn.fileManager
}

// GetStorageEngine returns the storage engine instance
func (dn *DecentralizedNode) GetStorageEngine() *storage.Engine {
	return dn.storage
}

// GetNetworkView returns the network view for the dn node
func (dn *DecentralizedNode) NetworkView() *NetworkView {
	return dn.networkView
}

// GetReputation returns a copy of the reputation map (thread-safe)
func (dn *DecentralizedNode) GetReputation() *ReputationSystem {
	return dn.reputation
}

// GossipManager returns the gossip manager instance
func (dn *DecentralizedNode) GossipManager() *GossipManager {
	return dn.gossip
}

// Ctx returns the context for the decentralized node
func (dn *DecentralizedNode) Ctx() context.Context {
	return dn.ctx
}

func (dn *DecentralizedNode) GetPoSManager() *PoSManager {
	return dn.posManager
}

// GetNetworkStats returns network statistics
func (dn *DecentralizedNode) GetNetworkStats() map[string]interface{} {
	dn.networkView.mu.RLock()
	defer dn.networkView.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_peers"] = len(dn.networkView.peers)
	stats["known_files"] = len(dn.networkView.fileIndex)
	stats["last_updated"] = dn.networkView.lastUpdated
	stats["node_id"] = dn.node.Host.ID().String()

	// Get DHT stats
	routingTable := dn.dht.RoutingTable()
	if routingTable != nil {
		stats["dht_peers"] = routingTable.Size()
	}

	// Calculate average reputation
	var totalRep float64
	for _, rep := range dn.networkView.reputation {
		totalRep += rep
	}
	if len(dn.networkView.reputation) > 0 {
		stats["avg_reputation"] = totalRep / float64(len(dn.networkView.reputation))
	} else {
		stats["avg_reputation"] = 0.0
	}

	return stats
}

func (dn *DecentralizedNode) Close() error {
	dn.cancel()

	// Close components in order
	if dn.fileManager != nil {
		if err := dn.fileManager.Close(); err != nil {
			log.Printf("Error closing file manager: %v", err)
		}
	}

	if dn.gossip != nil {
		// Gossip cleanup would go here
	}

	if dn.discovery != nil {
		// Discovery cleanup would go here
	}

	if dn.dht != nil {
		if err := dn.dht.Close(); err != nil {
			log.Printf("Error closing DHT: %v", err)
		}
	}

	if dn.node != nil {
		if err := dn.node.Close(); err != nil {
			log.Printf("Error closing base node: %v", err)
		}
	}

	log.Println("Decentralized node shutdown complete")
	return nil
}
