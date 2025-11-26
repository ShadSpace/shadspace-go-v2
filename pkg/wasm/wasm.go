//go:build js && wasm
// +build js,wasm

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ShadSpaceSDK represents the WebAssembly-compatible SDK
type ShadSpaceSDK struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Config represents the SDK configuration
type Config struct {
	Port           int      `json:"port"`
	BootstrapPeers []string `json:"bootstrapPeers"`
	NodeType       string   `json:"nodeType"`
}

// NewShadSpaceSDK creates a new SDK instance
func NewShadSpaceSDK() *ShadSpaceSDK {
	ctx, cancel := context.WithCancel(context.Background())
	return &ShadSpaceSDK{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Init initializes the SDK with configuration
func (s *ShadSpaceSDK) Init(configJSON string) (string, error) {
	var config Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return "", fmt.Errorf("failed to parse config: %w", err)
	}

	// Simulate initialization delay
	time.Sleep(1 * time.Second)

	response := map[string]interface{}{
		"nodeId": "wasm-node-" + fmt.Sprintf("%d", time.Now().Unix()),
		"status": "initialized",
		"config": config,
	}

	return s.toJSON(response)
}

// UploadFile uploads a file to the network
func (s *ShadSpaceSDK) UploadFile(fileData []byte, fileName string, totalShards, requiredShards int) (string, error) {
	// Simulate file upload
	time.Sleep(2 * time.Second)

	fileHash := fmt.Sprintf("hash_%d", time.Now().UnixNano())

	response := map[string]interface{}{
		"fileHash": fileHash,
		"fileName": fileName,
		"size":     len(fileData),
		"shards": map[string]int{
			"total":    totalShards,
			"required": requiredShards,
		},
		"message": "File uploaded successfully (simulated)",
	}

	return s.toJSON(response)
}

// DownloadFile downloads a file from the network
func (s *ShadSpaceSDK) DownloadFile(fileHash string) (string, error) {
	// Simulate file download
	time.Sleep(1 * time.Second)

	// Create mock file data
	mockData := []byte("This is simulated file content for hash: " + fileHash)

	response := map[string]interface{}{
		"fileHash": fileHash,
		"data":     mockData,
		"size":     len(mockData),
		"message":  "File downloaded successfully (simulated)",
	}

	return s.toJSON(response)
}

// ListFiles lists all known files
func (s *ShadSpaceSDK) ListFiles() (string, error) {
	// Return mock file list
	files := []map[string]interface{}{
		{
			"fileHash":  "abc123",
			"fileName":  "test1.txt",
			"fileSize":  1024,
			"shards":    3,
			"required":  2,
			"createdAt": time.Now().Format(time.RFC3339),
			"peers":     2,
		},
		{
			"fileHash":  "def456",
			"fileName":  "test2.jpg",
			"fileSize":  2048,
			"shards":    4,
			"required":  3,
			"createdAt": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			"peers":     3,
		},
	}

	response := map[string]interface{}{
		"files": files,
		"count": len(files),
	}

	return s.toJSON(response)
}

// GetNetworkStats returns network statistics
func (s *ShadSpaceSDK) GetNetworkStats() (string, error) {
	// Return mock network stats
	response := map[string]interface{}{
		"network": map[string]interface{}{
			"peers":         5,
			"knownFiles":    12,
			"dhtPeers":      8,
			"avgReputation": 0.85,
		},
		"storage": map[string]interface{}{
			"totalFiles":   2,
			"totalSize":    3072,
			"pendingOps":   0,
			"completedOps": 15,
		},
	}

	return s.toJSON(response)
}

// GetNodeInfo returns node information
func (s *ShadSpaceSDK) GetNodeInfo() (string, error) {
	response := map[string]interface{}{
		"nodeId":    "wasm-node-123",
		"peerId":    "12D3KooWSimulatedNode",
		"addresses": []string{"/ip4/127.0.0.1/tcp/4001"},
		"status":    "connected",
		"type":      "wasm",
	}

	return s.toJSON(response)
}

// Close shuts down the SDK
func (s *ShadSpaceSDK) Close() error {
	s.cancel()
	return nil
}

// Helper function to convert to JSON
func (s *ShadSpaceSDK) toJSON(data interface{}) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(jsonData), nil
}
