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
	"github.com/libp2p/go-libp2p/core/network"
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

	// Peer connection events
	connectionEvents chan peer.ID
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
		node:             node,
		ctx:              masterCtx,
		cancel:           cancel,
		farmers:          make(map[peer.ID]*types.FarmerInfo),
		fileIndex:        make(map[string][]peer.ID),
		metrics:          &types.NetworkMetrics{},
		connectionEvents: make(chan peer.ID, 100), // Buffered channel for connection events
	}

	node.SetHandler(protocol.NewMessageHandler(master))

	// Start API server
	master.startAPIServer(8080)
	master.setupConnectionMonitoring()

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

	// Register the farmer with enhanced information
	addresses := req.Addresses
	if len(addresses) == 0 {
		// If no addresses provided, use the connection address
		if conns := mn.node.Host.Network().ConnsToPeer(peerID); len(conns) > 0 {
			addresses = []string{conns[0].RemoteMultiaddr().String() + "/p2p/" + peerID.String()}
		}
	}

	// Use default values if not provided in request
	location := req.Location
	if location == "" {
		location = "Unknown"
	}

	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = "Node-" + peerID.String()[:8] // Generate default node name
	}

	version := req.Version
	if version == "" {
		version = "2.1.4" // Default version
	}

	if err := mn.RegisterFarmer(peerID, req.StorageCapacity, addresses, location, nodeName, version); err != nil {
		return fmt.Errorf("failed to register farmer: %w", err)
	}

	// Update metrics
	mn.metrics.TotalFarmers++
	mn.metrics.ActiveFarmers++
	mn.metrics.TotalStorage += req.StorageCapacity

	log.Printf("Farmer %s registered with %d MB capacity in %s",
		peerID, req.StorageCapacity/(1024*1024), location)

	return nil
}

// ChangeIsActiveStatus for inactive farmers to false
func (mn *MasterNode) ChangeIsActiveStatusForFarmers() {
	mn.farmersMu.Lock()
	defer mn.farmersMu.Unlock()

	now := time.Now()
	inactiveThreshold := 5 * time.Minute // Reduced from 10 to 5 minutes

	for peerID, farmer := range mn.farmers {
		// Check if disconnected via network
		if mn.node.Host.Network().Connectedness(peerID) != network.Connected {
			if farmer.IsActive {
				farmer.IsActive = false
				farmer.Tags = updateTags(farmer.Tags, "offline", true)
				farmer.Tags = updateTags(farmer.Tags, "online", false)
				log.Printf("Marked farmer %s as inactive (network disconnected)", peerID)
			}
			continue
		}

		// Check last seen time
		if now.Sub(farmer.LastSeen) > inactiveThreshold {
			farmer.IsActive = false
			farmer.Tags = updateTags(farmer.Tags, "offline", true)
			farmer.Tags = updateTags(farmer.Tags, "online", false)
			log.Printf("Marked farmer %s as inactive (last seen %v ago)",
				peerID, now.Sub(farmer.LastSeen))
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
	farmer.LastSync = time.Now()
	farmer.Latency = proof.Latency
	farmer.Uptime = proof.Uptime

	if proof.Location != "" && proof.Location != "unknown" {
		farmer.Location = proof.Location
	}

	// Recalculate reliability
	reliability := mn.calculateReliability(proof)
	farmer.Tags = mn.generateFarmerTags(farmer)

	// changes status to active upon receiving proof
	if !farmer.IsActive {
		farmer.IsActive = true
		farmer.Tags = updateTags(farmer.Tags, "online", true)
		farmer.Tags = updateTags(farmer.Tags, "offline", false)
	}

	// Update network metrics
	mn.updateNetworkMetrics()

	log.Printf("Updated farmer %s: %d/%d MB used, reliability: %.2f, location: %s",
		peerID, proof.UsedStorage/(1024*1024), farmer.StorageCapacity/(1024*1024),
		reliability, proof.Location)
}

// calculateReliability calculates farmer reliability based on proof data
func (mn *MasterNode) calculateReliability(proof types.ProofOfStorage) float64 {
	// Base reliability from uptime (convert seconds to percentage)
	uptimeScore := proof.Uptime / (24 * 60 * 60) // Convert seconds to days, assume 100% if > 1 day
	if uptimeScore > 1.0 {
		uptimeScore = 1.0
	}

	// Latency score (lower latency is better)
	latencyScore := 1.0
	if proof.Latency > 0 {
		latencyScore = 1.0 - (float64(proof.Latency) / 1000.0) // Max 1000ms latency
		if latencyScore < 0 {
			latencyScore = 0
		}
	}

	// Storage health (based on usage percentage)
	storageHealth := 1.0
	if proof.AvailableStorage > 0 {
		usagePercent := float64(proof.UsedStorage) / float64(proof.AvailableStorage)
		// Ideal usage is between 20-80%
		if usagePercent < 0.2 || usagePercent > 0.8 {
			storageHealth = 0.8
		}
	}

	// Combine scores
	reliability := (uptimeScore + latencyScore + storageHealth) / 3.0
	return reliability
}

// generateFarmerTags generates tags for frontend display
func (mn *MasterNode) generateFarmerTags(farmer *types.FarmerInfo) []string {
	tags := []string{"farmer"}

	// Status tags
	if farmer.IsActive {
		tags = append(tags, "online")
	} else {
		tags = append(tags, "offline")
	}

	// Reliability tags
	if farmer.Reliability > 0.8 {
		tags = append(tags, "high-performance")
	} else if farmer.Reliability < 0.5 {
		tags = append(tags, "degraded")
	}

	// Storage usage tags
	usagePercent := float64(farmer.UsedStorage) / float64(farmer.StorageCapacity)
	if usagePercent > 0.8 {
		tags = append(tags, "high-usage")
	} else if usagePercent < 0.2 {
		tags = append(tags, "low-usage")
	}

	if farmer.Location != "" && farmer.Location != "unknown" {
		tags = append(tags, "geo-enabled")
	}

	if farmer.Latency > 500 {
		tags = append(tags, "high-latency")
	} else if farmer.Latency < 50 {
		tags = append(tags, "low-latency")
	}

	return tags
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
			mn.ChangeIsActiveStatusForFarmers()
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

// updateNetworkMetrics updates comprehensive network metrics
func (mn *MasterNode) updateNetworkMetrics() {
	mn.farmersMu.RLock()
	defer mn.farmersMu.RUnlock()

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
		activeFarmers = append(activeFarmers, farmer)
	}
	return activeFarmers
}

// RegisterFarmer registers a new farmer node
func (mn *MasterNode) RegisterFarmer(peerID peer.ID, capacity uint64, addresses []string, location, nodeName, version string) error {
	mn.farmersMu.Lock()
	defer mn.farmersMu.Unlock()

	farmer := &types.FarmerInfo{
		PeerID:          peerID,
		StorageCapacity: capacity,
		UsedStorage:     0,
		Reliability:     1.0,
		LastSeen:        time.Now(),
		LastSync:        time.Now(),
		IsActive:        true,
		Addresses:       addresses,
		Location:        location,
		NodeName:        nodeName,
		Version:         version,
		StartTime:       time.Now(),
		Tags:            []string{"farmer", "online"},
	}

	mn.farmers[peerID] = farmer

	log.Printf("Registered farmer: %s", peerID)
	return nil
}

// updateTags helper function to update tags
func updateTags(tags []string, tag string, add bool) []string {
	if add {
		// Add tag if not present
		for _, t := range tags {
			if t == tag {
				return tags
			}
		}
		return append(tags, tag)
	} else {
		// Remove tag if present
		newTags := []string{}
		for _, t := range tags {
			if t != tag {
				newTags = append(newTags, t)
			}
		}
		return newTags
	}
}

func (mn *MasterNode) setupConnectionMonitoring() {
	// Monitor connection state changes
	mn.node.Host.Network().Notify(&network.NotifyBundle{
		DisconnectedF: func(net network.Network, conn network.Conn) {
			peerID := conn.RemotePeer()
			select {
			case mn.connectionEvents <- peerID:
				log.Printf("Farmer %s disconnected", peerID)
			default:
				// Channel full, log and continue
				log.Printf("Farmer %s disconnected (event channel full)", peerID)
			}
		},
	})

	// Start connection monitor goroutine
	go mn.monitorConnections()
}

// monitorConnections handles connection events
func (mn *MasterNode) monitorConnections() {
	for {
		select {
		case <-mn.ctx.Done():
			return
		case peerID := <-mn.connectionEvents:
			mn.handleFarmerDisconnect(peerID)
		}
	}
}

// handleFarmerDisconnect updates farmer status when they disconnect
func (mn *MasterNode) handleFarmerDisconnect(peerID peer.ID) {
	mn.farmersMu.Lock()
	defer mn.farmersMu.Unlock()

	farmer, exists := mn.farmers[peerID]
	if !exists {
		return
	}

	// Mark as inactive
	farmer.IsActive = false
	farmer.LastSeen = time.Now()
	farmer.Tags = updateTags(farmer.Tags, "online", false)
	farmer.Tags = updateTags(farmer.Tags, "offline", true)

	// Update metrics
	mn.metrics.ActiveFarmers--
	if mn.metrics.ActiveFarmers < 0 {
		mn.metrics.ActiveFarmers = 0
	}

	log.Printf("Farmer %s marked as offline due to disconnection", peerID)
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
