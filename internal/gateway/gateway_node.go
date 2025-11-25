// internal/gateway/gateway_node.go
package gateway

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/api"
	"github.com/ShadSpace/shadspace-go-v2/internal/network"
	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
	"github.com/libp2p/go-libp2p/core/peer"
)

// GatewayNode represents a specialized node that serves as an API gateway
type GatewayNode struct {
	node        *network.DecentralizedNode
	apiServer   *api.APIServer
	ctx         context.Context
	cancel      context.CancelFunc
	startTime   time.Time
	gatewayInfo *types.GatewayInfo
}

// GatewayInfo contains gateway-specific information
type GatewayInfo struct {
	PeerID         peer.ID   `json:"peer_id"`
	NodeName       string    `json:"node_name"`
	APIPort        int       `json:"api_port"`
	NodePort       int       `json:"node_port"`
	Version        string    `json:"version"`
	IsActive       bool      `json:"is_active"`
	StartTime      time.Time `json:"start_time"`
	LastSeen       time.Time `json:"last_seen"`
	ConnectedPeers int       `json:"connected_peers"`
	TotalRequests  int64     `json:"total_requests"`
	Uptime         string    `json:"uptime,omitempty"`
}

// NewGatewayNode creates a new gateway node
func NewGatewayNode(ctx context.Context, config network.NodeConfig, apiPort int) (*GatewayNode, error) {
	nodeCtx, cancel := context.WithCancel(ctx)

	// Create the underlying decentralized node
	decentralizedNode, err := network.NewDecentralizedNode(nodeCtx, config)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create decentralized node: %w", err)
	}

	// Create gateway info
	gatewayInfo := &types.GatewayInfo{
		PeerID:    decentralizedNode.GetPeerID(),
		NodeName:  utils.GenerateNodeName(decentralizedNode.GetPeerID()) + "-gateway",
		APIPort:   apiPort,
		NodePort:  config.Port,
		Version:   "1.0.0",
		IsActive:  true,
		StartTime: time.Now(),
		LastSeen:  time.Now(),
	}

	gateway := &GatewayNode{
		node:        decentralizedNode,
		ctx:         nodeCtx,
		cancel:      cancel,
		startTime:   time.Now(),
		gatewayInfo: gatewayInfo,
	}

	// Create API server
	gateway.apiServer = api.NewAPIServer(gateway, apiPort)

	return gateway, nil
}

// Start begins the gateway node operations
func (gn *GatewayNode) Start() error {
	log.Printf("🚀 Starting ShadSpace Gateway Node")
	log.Printf("📡 Node ID: %s", gn.node.GetHostID())
	log.Printf("🔌 P2P Port: %d", gn.gatewayInfo.NodePort)
	log.Printf("🌐 API Port: %d", gn.gatewayInfo.APIPort)

	// Wait for network to initialize
	time.Sleep(2 * time.Second)

	// Start the API server in a goroutine
	go func() {
		if err := gn.apiServer.Start(); err != nil {
			log.Printf("❌ API server failed: %v", err)
		}
	}()

	// Start background monitoring
	go gn.monitorGateway()

	log.Printf("✅ Gateway node started successfully")
	return nil
}

// monitorGateway monitors gateway health and statistics
func (gn *GatewayNode) monitorGateway() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-gn.ctx.Done():
			return
		case <-ticker.C:
			gn.updateGatewayStats()
		}
	}
}

// updateGatewayStats updates gateway statistics
func (gn *GatewayNode) updateGatewayStats() {
	gn.gatewayInfo.LastSeen = time.Now()

	// Get network stats
	networkStats := gn.node.GetNetworkStats()
	gn.gatewayInfo.ConnectedPeers = networkStats["total_peers"].(int)

	// Update uptime
	gn.gatewayInfo.Uptime = time.Since(gn.startTime).String()
}

// GetNode returns the underlying decentralized node
func (gn *GatewayNode) GetNode() *network.DecentralizedNode {
	return gn.node
}

// GetGatewayInfo returns gateway information
func (gn *GatewayNode) GetGatewayInfo() *types.GatewayInfo {
	return gn.gatewayInfo
}

// GetNetworkStats returns network statistics
func (gn *GatewayNode) GetNetworkStats() map[string]interface{} {
	return gn.node.GetNetworkStats()
}

// GetStorageStats returns storage statistics
func (gn *GatewayNode) GetStorageStats() map[string]interface{} {
	fileStats := gn.node.GetFileManager().GetFileStats()
	storageStats := gn.node.GetStorageEngine().GetStats()

	return map[string]interface{}{
		"files":   fileStats,
		"storage": storageStats,
		"gateway": map[string]interface{}{
			"total_requests": gn.gatewayInfo.TotalRequests,
			"uptime":         gn.gatewayInfo.Uptime,
		},
	}
}

// StoreFile stores a file through the gateway
func (gn *GatewayNode) StoreFile(filename string, data []byte, totalShards, requiredShards int) (string, error) {
	gn.gatewayInfo.TotalRequests++
	return gn.node.GetStorageOperations().StoreFileDistributed(filename, data, totalShards, requiredShards)
}

// RetrieveFile retrieves a file through the gateway
func (gn *GatewayNode) RetrieveFile(fileHash string) ([]byte, error) {
	gn.gatewayInfo.TotalRequests++
	return gn.node.GetStorageOperations().RetrieveFileDistributed(fileHash)
}

// DeleteFile deletes a file through the gateway
func (gn *GatewayNode) DeleteFile(fileHash string) error {
	gn.gatewayInfo.TotalRequests++
	return gn.node.GetStorageOperations().DeleteFileDistributed(fileHash)
}

// GetFileList returns list of files available through this gateway
func (gn *GatewayNode) GetFileList() []*storage.FileMetadata {
	return gn.node.GetFileManager().ListFiles()
}

// GetFileInfo returns information about a specific file
func (gn *GatewayNode) GetFileInfo(fileHash string) (*storage.FileMetadata, error) {
	return gn.node.GetFileManager().GetFileInfo(fileHash)
}

// GetNodeList returns information about nodes in the network
func (gn *GatewayNode) GetNodeList() []types.NodeInfo {
	// This would aggregate node information from the network view
	// For now, return basic information
	networkView := gn.node.NetworkView()
	peers := networkView.GetPeers()

	nodeInfos := make([]types.NodeInfo, 0, len(peers)+1)

	// Add self
	nodeInfos = append(nodeInfos, types.NodeInfo{
		ID:              gn.node.GetHostID(),
		NodeType:        "gateway",
		IsActive:        true,
		StorageCapacity: 10 * 1024 * 1024 * 1024, // 10GB
		UsedStorage:     gn.getUsedStorage(),
		Reliability:     1.0,
		LastSeen:        time.Now(),
		Tags:            []string{"gateway", "api"},
	})

	// Add other peers (include all peers, even those without gossiped info)
	for peerID, peerInfo := range peers {
		nodeInfo := types.NodeInfo{
			ID:          peerID.String(),
			NodeType:    "farmer",
			IsActive:    true, // Assume active if in network view
			Reliability: peerInfo.Reliability,
			LastSeen:    peerInfo.LastSeen,
		}

		if peerInfo.Info != nil {
			// Use gossiped info if available
			nodeInfo.IsActive = peerInfo.Info.IsActive
			nodeInfo.StorageCapacity = peerInfo.Info.StorageCapacity
			nodeInfo.UsedStorage = peerInfo.Info.UsedStorage
			nodeInfo.Reliability = peerInfo.Info.Reliability
			nodeInfo.Addresses = peerInfo.Info.Addresses
			nodeInfo.Tags = peerInfo.Info.Tags
		} else {
			// Use default values for peers without gossiped info
			nodeInfo.StorageCapacity = 0
			nodeInfo.UsedStorage = 0
			nodeInfo.Reliability = 0.5 // Default reliability
			nodeInfo.Tags = []string{"unknown"}
		}

		nodeInfos = append(nodeInfos, nodeInfo)
	}

	return nodeInfos
}

// GetSystemStatus returns overall system status
func (gn *GatewayNode) GetSystemStatus() map[string]interface{} {
	networkStats := gn.node.GetNetworkStats()
	fileStats := gn.node.GetFileManager().GetFileStats()

	return map[string]interface{}{
		"status":    "operational",
		"timestamp": time.Now(),
		"components": map[string]interface{}{
			"p2p_network": map[string]interface{}{
				"status":    "connected",
				"peers":     networkStats["total_peers"],
				"dht_nodes": networkStats["dht_peers"],
			},
			"file_storage": map[string]interface{}{
				"status":     "active",
				"files":      fileStats["total_files"],
				"total_size": fileStats["total_file_size"],
			},
			"api_gateway": map[string]interface{}{
				"status": "running",
				"uptime": gn.gatewayInfo.Uptime,
			},
		},
		"gateway": gn.gatewayInfo,
	}
}

// Close shuts down the gateway node
func (gn *GatewayNode) Close() error {
	gn.cancel()

	// Shutdown API server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := gn.apiServer.Shutdown(ctx); err != nil {
		log.Printf("Warning: API server shutdown failed: %v", err)
	}

	// Close the underlying node
	if err := gn.node.Close(); err != nil {
		return fmt.Errorf("failed to close node: %w", err)
	}

	log.Println("Gateway node shutdown complete")
	return nil
}

// Helper method
func (gn *GatewayNode) getUsedStorage() uint64 {
	stats := gn.node.GetFileManager().GetFileStats()
	if used, ok := stats["total_file_size"]; ok {
		return uint64(used.(int64))
	}
	return 0
}
