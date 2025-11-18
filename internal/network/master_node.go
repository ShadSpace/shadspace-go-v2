package network

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	// "github.com/libp2p/go-libp2p/core/peerstore"
)


// MasterNode represent the master node in the Shadspace network
type MasterNode struct {
	node        *Node
	ctx   	    context.Context
	cancel      context.CancelFunc

	// Famrer managment
	farmers 	map[peer.ID]*FarmerInfo
	farmersMu	sync.RWMutex

	// File index tracking
	fileIndex 	map[string][]peer.ID // file hash -> farmer peers
	fileIndexMu sync.RWMutex

	//Network metrics
	metrics   	*NetworkMetrics
}

// FarmerInfo contains information about a farmer node
type FarmerInfo struct {
	PeerID 		        peer.ID
	StorageCapacity     uint64
	UseStorage       	uint64
	Reliability       	float64
	LastSeen			time.Time
	isActive			bool
	Addresses			[]string
}

// NetworkMetrics track network-wide metrics
type NetworkMetrics struct {
	TotalFarmers       int
	ActiveFarmers	   int
	TotalStorage   	   uint64
	UsedStorage		   uint64
	FilesStorage   	   int
	UploadsToday	   int
	DownloadsToday	   int
}


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
		farmers:   make(map[peer.ID]*FarmerInfo),
		fileIndex: make(map[string][]peer.ID),
		metrics:    &NetworkMetrics{},
	}


	// TODO: start background tasks monitorFarmers, manageDiscovery, collectMetrics

	log.Printf("Master node initialized with ID: %s", node.Host.ID())
	return master, nil
}

// Shutdown gracefully shuts down the master node
func (mn *MasterNode) Shutdown() error {
	mn.cancel()
	return mn.node.Close()
}