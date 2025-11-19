package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/api"
	"github.com/ShadSpace/shadspace-go-v2/internal/protocol"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// MasterNode represents the master node in the Shadspace network
type MasterNode struct {
	node      *Node
	ctx       context.Context
	cancel    context.CancelFunc
	apiServer *api.APIServer

	// Farmer management - using types.FarmerInfo
	farmers   map[peer.ID]*types.FarmerInfo
	farmersMu sync.RWMutex

	// File index tracking
	fileIndex   map[string][]peer.ID // file hash -> farmer peers
	fileIndexMu sync.RWMutex

	// Network metrics - using types.NetworkMetrics
	metrics *types.NetworkMetrics
}

// Ensure MasterNode implements the interface
var _ types.MasterNodeInterface = (*MasterNode)(nil)
var _ protocol.MasterNodeHandler = (*MasterNode)(nil)

// NewMasterNode creates and starts a new master node
func NewMasterNode(ctx context.Context, config NodeConfig) (*MasterNode, error) {
	// Create context for master node
	masterCtx, cancel := context.WithCancel(ctx)

	// Create base node
	node, err := NewNode(masterCtx, config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create base node: %w", err)
	}

	master := &MasterNode{
		node:      node,
		ctx:       masterCtx,
		cancel:    cancel,
		farmers:   make(map[peer.ID]*types.FarmerInfo),
		fileIndex: make(map[string][]peer.ID),
		metrics:   &types.NetworkMetrics{},
	}

	node.SetHandler(protocol.NewMessageHandler(master))

	// Start API server
	master.startAPIServer(8080)

	go master.startBackgroundTasks()

	// TODO: start background tasks monitorFarmers, manageDiscovery, collectMetrics

	log.Printf("Master node initialized with ID: %s", node.Host.ID())
	return master, nil
}

// startAPIServer starts the HTTP API server
func (mn *MasterNode) startAPIServer(port int) {
	mn.apiServer = api.NewAPIServer(mn, port)

	go func() {
		if err := mn.apiServer.Start(); err != nil {
			log.Printf("API server error: %v", err)
		}
	}()
}

// handleIncomingRegistration handles farmer registration requests
func (mn *MasterNode) HandleIncomingRegistration(peerID peer.ID, req types.RegistrationRequest) error {
	// Validate registration request
	if req.StorageCapacity == 0 {
		return fmt.Errorf("invalid storage capacity")
	}

	// Register the farmer
	addresses := req.Addresses
	if len(addresses) == 0 {
		// If no addresses provided, use the connection address
		if conns := mn.node.Host.Network().ConnsToPeer(peerID); len(conns) > 0 {
			// FIXED: Added missing slash in p2p address
			addresses = []string{conns[0].RemoteMultiaddr().String() + "/p2p/" + peerID.String()}
		}
	}

	if err := mn.RegisterFarmer(peerID, req.StorageCapacity, addresses); err != nil {
		return fmt.Errorf("failed to register farmer: %w", err)
	}

	// Update metrics
	mn.metrics.TotalFarmers++
	mn.metrics.ActiveFarmers++
	mn.metrics.TotalStorage += req.StorageCapacity

	log.Printf("Farmer %s registered with %d MB capacity", peerID, req.StorageCapacity/(1024*1024))

	return nil
}

// RemoveInactiveFarmers marks farmers as inactive if they haven't reported in a while
func (mn *MasterNode) RemoveInactiveFarmers() {
	mn.farmersMu.Lock()
	defer mn.farmersMu.Unlock()

	now := time.Now()
	inactiveThreshold := 10 * time.Minute

	for peerID, farmer := range mn.farmers {
		if now.Sub(farmer.LastSeen) > inactiveThreshold {
			farmer.IsActive = false
			mn.metrics.ActiveFarmers--
			log.Printf("Marked farmer %s as inactive", peerID)
		}
	}
}

// HandleProofOfStorage processes proofs of storage from farmers (implements protocol.MasterNodeHandler)
func (mn *MasterNode) HandleProofOfStorage(peerID peer.ID, proof types.ProofOfStorage) {
	mn.farmersMu.Lock()
	defer mn.farmersMu.Unlock()

	farmer, exists := mn.farmers[peerID]
	if !exists {
		log.Printf("Received proof from unregistered farmer: %s", peerID)
		return
	}

	// Update farmer information
	farmer.UsedStorage = proof.UsedStorage
	farmer.LastSeen = time.Now()
	farmer.Reliability = proof.Metrics.Reliability

	// Update metrics (note: this should aggregate across all farmers)
	// For now, we'll just track the latest value
	mn.metrics.UsedStorage = proof.UsedStorage

	log.Printf("Updated farmer %s: %d/%d MB used, reliability: %.2f",
		peerID, proof.UsedStorage/(1024*1024), farmer.StorageCapacity/(1024*1024), proof.Metrics.Reliability)
}

// HandleStorageOffer processes storage offers from farmers (implements protocol.MasterNodeHandler)
func (mn *MasterNode) HandleStorageOffer(peerID peer.ID, offer types.StorageOffer) {
	mn.fileIndexMu.Lock()
	defer mn.fileIndexMu.Unlock()

	// Add farmer to file index
	existingFarmers := mn.fileIndex[offer.ChunkHash]

	// Check if farmer is already in the list
	found := false
	for _, existingPeer := range existingFarmers {
		if existingPeer == peerID {
			found = true
			break
		}
	}

	if !found {
		mn.fileIndex[offer.ChunkHash] = append(existingFarmers, peerID)
		log.Printf("Storage offer for chunk %s from farmer %s", offer.ChunkHash, peerID)
	}
}

// startBackgroundTasks starts maintenance tasks for master node
func (mn *MasterNode) startBackgroundTasks() {
	cleanupTicker := time.NewTicker(2 * time.Minute)
	defer cleanupTicker.Stop()

	metricsTicker := time.NewTicker(30 * time.Second)
	defer metricsTicker.Stop()

	for {
		select {
		case <-mn.ctx.Done():
			return
		case <-cleanupTicker.C:
			mn.RemoveInactiveFarmers()
		case <-metricsTicker.C:
			mn.updateMetrics()
		}
	}
}

// updateMetrics updates network metrics
func (mn *MasterNode) updateMetrics() {
	mn.farmersMu.RLock()
	defer mn.farmersMu.RUnlock()

	// Count active farmers and calculate total storage
	activeCount := 0
	totalStorage := uint64(0)
	usedStorage := uint64(0)

	for _, farmer := range mn.farmers {
		if farmer.IsActive {
			activeCount++
			totalStorage += farmer.StorageCapacity
			usedStorage += farmer.UsedStorage
		}
	}

	mn.metrics.ActiveFarmers = activeCount
	mn.metrics.TotalStorage = totalStorage
	mn.metrics.UsedStorage = usedStorage
}

// GetHost returns the underlying libp2p host
func (mn *MasterNode) GetHost() host.Host {
	return mn.node.Host
}

// GetMetrics returns current network metrics
func (mn *MasterNode) GetMetrics() *types.NetworkMetrics {
	return mn.metrics
}

// GetFarmers returns a list of active farmers
func (mn *MasterNode) GetFarmers() []*types.FarmerInfo {
	mn.farmersMu.RLock()
	defer mn.farmersMu.RUnlock()

	var activeFarmers []*types.FarmerInfo
	for _, farmer := range mn.farmers {
		if farmer.IsActive {
			activeFarmers = append(activeFarmers, farmer)
		}
	}
	return activeFarmers
}

// RegisterFarmer registers a new farmer node
func (mn *MasterNode) RegisterFarmer(peerID peer.ID, capacity uint64, addresses []string) error {
	mn.farmersMu.Lock()
	defer mn.farmersMu.Unlock()

	farmer := &types.FarmerInfo{
		PeerID:          peerID,
		StorageCapacity: capacity,
		UsedStorage:     0,
		Reliability:     1.0,
		LastSeen:        time.Now(),
		IsActive:        true,
		Addresses:       addresses,
	}

	mn.farmers[peerID] = farmer
	log.Printf("Registered farmer: %s", peerID)
	return nil
}

// Shutdown gracefully shuts down the master node
func (mn *MasterNode) Shutdown() error {
	// Shutdown API server first
	if mn.apiServer != nil {
		apiCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mn.apiServer.Shutdown(apiCtx)
	}

	mn.cancel()
	return mn.node.Close()
}
