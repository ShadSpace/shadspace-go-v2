package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// FarmerNode represents a farmer node node in the Shadspace network
type FarmerNode struct {
	node          *Node
	ctx           context.Context
	cancel 		  context.CancelFunc
	storage       *storage.Engine
	masterPeer    peer.ID
	isRegistered  bool
	mu            sync.RWMutex

	// storage metrics
	storageCapacity    uint64
	usedStorage        uint64
}

// NewFarmerNode creates and starts a new farmer node
func NewFarmerNode(ctx context.Context, config NodeConfig) (*FarmerNode, error) {
	// create context for farmer node
	farmerCtx, cancel := context.WithCancel(ctx)

	// Create base node
	node, err := NewNode(farmerCtx, config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create base node: %w", err)
	}

	// Initialize storage engine
	storageEngine, err := storage.NewEngine()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to  create base node: %w", err)
	}

	farmer := &FarmerNode{
		node:              node,
		ctx:               farmerCtx,
		cancel:            cancel,
		storage:           storageEngine,
		isRegistered:      false,
		storageCapacity:   10 * 1024 * 1024 * 1024, // 10GB default
		usedStorage:       0,
	}

	// TODO: start background tasks
	go farmer.connectToMaster()

	log.Printf("Farmer node initialized with ID: %s", node.Host.ID())
	return farmer, nil
}

// contectToMaster attempt to connect to the master node
func (fn *FarmerNode) connectToMaster() {
	// Wait a bit for the node to fully initialize
	time.Sleep(2 * time.Second)

	if len(fn.node.Config.BootstrapPeers) == 0 {
		log.Println("No bootstrap peers configured, running in standalone mode")
		return
	}

	// Try to connect to bootstrap peers (master nodes)
	for _, peerAddr := range fn.node.Config.BootstrapPeers {
		if err := fn.connectToPeer(peerAddr); err != nil {
			log.Printf("Failed to coneect to peer %s: %v", peerAddr, err)
			continue
		}
	}

	// Register with master node 
	if err := fn.registerWithMaster(); err != nil {
		log.Printf("Failed to register with master: %v", err)
	} else {
		log.Printf("Successfully registered with master node")
	}
}

// connectToPeer connects to a specific peer
func (fn *FarmerNode) connectToPeer(peerAddr string) error {
	ma, err := multiaddr.NewMultiaddr(peerAddr)
	if err != nil {
		return fmt.Errorf("invalid multiaddress: %w", err)
	}

	addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return fmt.Errorf("failed to parse peer info: %w", err)
	}

	// Try to connect with timeout
	connectCtx, cancel := context.WithTimeout(fn.ctx, 30*time.Second)
	defer cancel()

	if err := fn.node.Host.Connect(connectCtx, *addrInfo); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	fn.mu.Lock()
	fn.masterPeer = addrInfo.ID
	fn.mu.Unlock()

	log.Printf("Connected to master node: %s", addrInfo.ID)
	return nil
}

// registerWithMaster registers this farmer with the master node
// registerWithMaster registers this farmer with the master node
func (fn *FarmerNode) registerWithMaster() error {
	fn.mu.RLock()
	masterPeer := fn.masterPeer
	fn.mu.RUnlock()

	if masterPeer == "" {
		return fmt.Errorf("no master peer connected")
	}

	log.Printf("Attempting to register with master node: %s", masterPeer)

	stream, err := fn.node.Host.NewStream(fn.ctx, masterPeer, protocol.ID(fn.node.Config.ProtocolID+"/discovery"))
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Create registration request
	registrationReq := types.RegistrationRequest{
		Message: types.Message{
			Type:      types.TypeRegistrationRequest,
			ID:        fmt.Sprintf("reg-%d", time.Now().UnixNano()),
			Timestamp: time.Now(),
			PeerID:    fn.node.Host.ID(),
		},
		StorageCapacity: fn.storageCapacity,
		UsedStorage:     fn.usedStorage,
		Addresses:       fn.getAddresses(),
		ProtocolVersion: "1.0.0",
	}

	log.Printf("Sending registration request to master...")

	// Send registration request
	if err := json.NewEncoder(stream).Encode(registrationReq); err != nil {
		return fmt.Errorf("failed to send registration request: %w", err)
	}

	log.Printf("Waiting for registration response...")

	// Read response
	var response types.RegistrationResponse
	if err := json.NewDecoder(stream).Decode(&response); err != nil {
		return fmt.Errorf("failed to read registration response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("registration failed: %s", response.StatusMessage)
	}

	fn.mu.Lock()
	fn.isRegistered = true
	fn.mu.Unlock()

	log.Printf("Registration successful: %s", response.StatusMessage)
	return nil
}

// getAddresses return the farmer`s address 
func (fn *FarmerNode) getAddresses() []string {
	var addresses []string
	for _, addr := range fn.node.Host.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, fn.node.Host.ID()))
	}
	return addresses
} 

// GetHost returns the underlying libp2p host
func (fn *FarmerNode) GetHost() host.Host {
	return fn.node.Host
}

// Shutdown gracefully shuts down the farmer node
func (fn *FarmerNode) Shutdown() error {
	fn.cancel()
	return fn.node.Close()
}