package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// FarmerNode represents a farmer node node in the Shadspace network
type FarmerNode struct {
	node         *Node
	ctx          context.Context
	cancel       context.CancelFunc
	storage      *storage.Engine
	masterPeer   peer.ID
	isRegistered bool
	mu           sync.RWMutex
	farmerInfo   *types.FarmerInfo
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

	farmerInfo := &types.FarmerInfo{
		PeerID:          node.Host.ID(),
		NodeName:        utils.GenerateNodeName(node.Host.ID()),
		Version:         "2.1.4",
		Addresses:       []string{},
		StorageCapacity: 10 * 1024 * 1024 * 1024, // 10GB default
		UsedStorage:     0,
		Reliability:     1.0,
		Location:        "unknown",
		IsActive:        false,
		Tags:            []string{"farmer"},
		StartTime:       time.Now(),
		LastSeen:        time.Now(),
		LastSync:        time.Now(),
	}

	farmer := &FarmerNode{
		node:         node,
		ctx:          farmerCtx,
		cancel:       cancel,
		storage:      storageEngine,
		farmerInfo:   farmerInfo,
		isRegistered: false,
	}

	// Start background process
	go farmer.connectToMaster()
	go farmer.detectLocation()
	go farmer.startMetricsCollection()
	go farmer.startProofOfStorageReporting()

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

	fn.updateFarmerInfo()

	// Create registration request
	registrationReq := types.RegistrationRequest{
		Message: types.Message{
			Type:      types.TypeRegistrationRequest,
			ID:        fmt.Sprintf("reg-%d", time.Now().UnixNano()),
			Timestamp: time.Now(),
			PeerID:    fn.node.Host.ID(),
		},
		StorageCapacity: fn.farmerInfo.StorageCapacity,
		UsedStorage:     fn.farmerInfo.UsedStorage,
		Addresses:       fn.farmerInfo.Addresses,
		ProtocolVersion: "1.0.0",
		Location:        fn.farmerInfo.Location,
		NodeName:        fn.farmerInfo.NodeName,
		Version:         fn.farmerInfo.Version,
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
	fn.farmerInfo.IsActive = true
	fn.farmerInfo.Tags = fn.generateTags()
	fn.mu.Unlock()

	log.Printf("Registration successful: %s", response.StatusMessage)
	return nil
}

// startMetricsCollection periodically updates farmer metrics
func (fn *FarmerNode) startMetricsCollection() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-fn.ctx.Done():
			return
		case <-ticker.C:
			fn.updateFarmerInfo()
		}
	}
}

// updateMetrics updates farmer performance and status metrics
func (fn *FarmerNode) updateFarmerInfo() {
	fn.mu.Lock()
	defer fn.mu.Unlock()

	// Update storage usage
	storageStats := fn.storage.GetStats()
	if usedBytes, ok := storageStats["used_bytes"].(uint64); ok {
		fn.farmerInfo.UsedStorage = usedBytes
	}

	// Update uptime
	fn.farmerInfo.Uptime = time.Since(fn.farmerInfo.StartTime).Seconds()

	// Update latency if connected to master
	if fn.masterPeer != "" {
		fn.farmerInfo.Latency = fn.measureLatency()
	}

	// Update addresses
	fn.farmerInfo.Addresses = fn.getAddresses()

	// Update tags
	fn.farmerInfo.Tags = fn.generateTags()

	fn.farmerInfo.LastSeen = time.Now()
}

// measureLatency measures latency to master node
func (fn *FarmerNode) measureLatency() int {
	if fn.masterPeer == "" {
		return 0
	}

	// sample ping measurement
	start := time.Now()
	ctx, cancel := context.WithTimeout(fn.ctx, 5*time.Second)
	defer cancel()

	stream, err := fn.node.Host.NewStream(ctx, fn.masterPeer, protocol.ID(fn.node.Config.ProtocolID+"/ping"))
	if err != nil {
		return 999 // High latency indicating connection issues
	}
	stream.Close()

	latency := time.Since(start).Milliseconds()
	return int(latency)
}

// getAddresses return the farmer`s address
func (fn *FarmerNode) getAddresses() []string {
	var addresses []string
	for _, addr := range fn.node.Host.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, fn.node.Host.ID()))
	}
	return addresses
}

// updateTags updates node tags based on current status
func (fn *FarmerNode) generateTags() []string {
	tags := []string{"farmer"}

	if fn.isRegistered {
		tags = append(tags, "registered", "online")
	} else {
		tags = append(tags, "unregistered")
	}

	// Performance-based tags
	if fn.farmerInfo.Latency > 500 {
		tags = append(tags, "high-latency")
	} else if fn.farmerInfo.Latency < 50 {
		tags = append(tags, "low-latency")
	}

	// Storage-based tags
	usagePercent := float64(fn.farmerInfo.UsedStorage) / float64(fn.farmerInfo.StorageCapacity) * 100
	if usagePercent > 80 {
		tags = append(tags, "storage-full")
	} else if usagePercent < 20 {
		tags = append(tags, "storage-available")
	}

	// Location-based tags
	if fn.farmerInfo.Location != "" && fn.farmerInfo.Location != "unknown" {
		tags = append(tags, "geo-enabled")
	}

	return tags
}

// detectLocation detects the geographical location of the farmer node
func (fn *FarmerNode) detectLocation() {
	location, err := fn.getLocationFromIP()
	if err != nil {
		log.Printf("Failed to detect location from IP: %v", err)
		fn.farmerInfo.Location = "unknown"
	} else {
		fn.mu.Lock()
		fn.farmerInfo.Location = location
		fn.mu.Unlock()
		log.Printf("Detected location: %s", location)
	}
}

// getLocationFromIP uses IP geolocation service to determine location
func (fn *FarmerNode) getLocationFromIP() (string, error) {
	// Use a free IP geolocation service
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get("https://ipapi.co/json/")
	if err != nil {
		// Fallback to ip-api.com
		return fn.getLocationFromIPAPI()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fn.getLocationFromIPAPI()
	}

	var locationData struct {
		City    string `json:"city"`
		Region  string `json:"region"`
		Country string `json:"country_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&locationData); err != nil {
		return fn.getLocationFromIPAPI()
	}

	// Format: "City, Region, Country"
	if locationData.City != "" && locationData.Country != "" {
		if locationData.Region != "" {
			return fmt.Sprintf("%s, %s, %s", locationData.City, locationData.Region, locationData.Country), nil
		}
		return fmt.Sprintf("%s, %s", locationData.City, locationData.Country), nil
	}

	return fn.getLocationFromIPAPI()
}

// getLocationFromIPAPI fallback geolocation service
func (fn *FarmerNode) getLocationFromIPAPI() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://ip-api.com/json/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var locationData struct {
		City    string `json:"city"`
		Region  string `json:"regionName"`
		Country string `json:"country"`
		Status  string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&locationData); err != nil {
		return "", err
	}

	if locationData.Status == "success" && locationData.City != "" {
		if locationData.Region != "" {
			return fmt.Sprintf("%s, %s, %s", locationData.City, locationData.Region, locationData.Country), nil
		}
		return fmt.Sprintf("%s, %s", locationData.City, locationData.Country), nil
	}
	return "", fmt.Errorf("failed to get location from IPAPI")
}

// startProofOfStorageReporting periodically reports proof of storage to master node
func (fn *FarmerNode) startProofOfStorageReporting() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-fn.ctx.Done():
			return
		case <-ticker.C:
			if fn.isRegistered {
				fn.sendProofOfStorage()
			}
		}
	}
}

// sendProofOfStorage sends a proof of storage message to the master node
func (fn *FarmerNode) sendProofOfStorage() {
	if fn.masterPeer == "" {
		return
	}

	stream, err := fn.node.Host.NewStream(fn.ctx, fn.masterPeer, protocol.ID(fn.node.Config.ProtocolID+"/proofofstorage"))
	if err != nil {
		log.Printf("Failed to send proof of storage: %v", err)
		fn.mu.Lock()
		fn.isRegistered = false
		fn.farmerInfo.IsActive = false
		fn.farmerInfo.Tags = fn.generateTags()
		fn.mu.Unlock()
		return
	}
	defer stream.Close()

	// Update farmer info before sending
	fn.updateFarmerInfo()

	proofOfStorage := types.ProofOfStorage{
		Message: types.Message{
			Type:      types.TypeProofOfStorage,
			ID:        fmt.Sprintf("pos-%d", time.Now().UnixNano()),
			Timestamp: time.Now(),
			PeerID:    fn.node.Host.ID(),
		},
		UsedStorage:      fn.farmerInfo.UsedStorage,
		AvailableStorage: fn.farmerInfo.StorageCapacity,
		ChunksStored:     0, // TODO: Get actual chunks stored
		Uptime:           fn.farmerInfo.Uptime,
		Location:         fn.farmerInfo.Location,
		Latency:          fn.farmerInfo.Latency,
	}

	if err := json.NewEncoder(stream).Encode(proofOfStorage); err != nil {
		log.Printf("Failed to encode proof of storage: %v", err)
	}
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
