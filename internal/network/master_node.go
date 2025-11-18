package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/api"
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

	// Start API server 
	master.startAPIServer(8080)

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

// Interface implementation

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