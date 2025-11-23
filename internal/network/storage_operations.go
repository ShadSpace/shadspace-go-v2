package network

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

type StorageOperations struct {
	node        *DecentralizedNode
	fileManager *storage.FileManager
	mu          sync.RWMutex
	pendingOps  map[string]*StorageOperation
}

type StorageOperation struct {
	FileHash    string
	Operation   string
	Status      string
	Shards      []*ShardTransfer
	StartTime   time.Time
	CompletedAt time.Time
	Error       string
}

type ShardTransfer struct {
	ShardIndex int
	Shardhash  string
	PeerID     peer.ID
	Status     string // "pending", "transferring", "completed", "failed"
	Size       int
}

// NewStorageOperations creates a new storage operations manager
func NewStorageOperations(node *DecentralizedNode, fileManeger *storage.FileManager) *StorageOperations {
	return &StorageOperations{
		node:        node,
		fileManager: fileManeger,
		pendingOps:  make(map[string]*StorageOperation),
	}
}

// StoraFileDistributed stores a file across the network
func (so *StorageOperations) StoreFileDistributed(fileName string, data []byte, totalShards, requiredShards int) (string, error) {
	storagePeers := so.selectStoragePeers(totalShards)
	if len(storagePeers) < totalShards {
		return "", fmt.Errorf("not enough available peers for storage: need %d, got %d",
			totalShards, len(storagePeers))
	}

	log.Printf("Selected %d peers for file storage", len(storagePeers))

	// Store file locally first (in production, this would distribute shards)
	fileHash, err := so.fileManager.StoreFile(fileName, data, totalShards, requiredShards, storagePeers)
	if err != nil {
		return "", fmt.Errorf("failed to store file: %w", err)
	}

	// Create storage operation record
	operation := &StorageOperation{
		FileHash:  fileHash,
		Operation: "store",
		Status:    "completed", // For now, since we're storing locally
		StartTime: time.Now(),
		Shards:    make([]*ShardTransfer, 0),
	}

	// Create shard transfer records (for tracking in distributed scenario)
	for i, peerID := range storagePeers {
		if i < totalShards {
			shardTransfer := &ShardTransfer{
				ShardIndex: i + 1,
				PeerID:     peerID,
				Status:     "completed", // Local storage
				Size:       len(data) / totalShards,
			}
			operation.Shards = append(operation.Shards, shardTransfer)
		}
	}

	so.mu.Lock()
	so.pendingOps[fileHash] = operation
	so.mu.Unlock()

	// Announce file to network
	so.announceFileToNetwork(fileHash, storagePeers)

	log.Printf("File %s stored and announced to network", fileHash)
	return fileHash, nil
}

// RetrieveFileDistributed retrieves a file from the network
func (so *StorageOperations) RetrieveFileDistributed(fileHash string) ([]byte, error) {
	// First try to retrieve from local storage
	data, err := so.fileManager.RetrieveFile(fileHash)
	if err == nil {
		log.Printf("File %s retrieved from local storage", fileHash)
		return data, nil
	}

	log.Printf("File %s not found locally, searching network...", fileHash)

	// Look for file in network view
	so.node.networkView.mu.RLock()
	storingPeers, exists := so.node.networkView.fileIndex[fileHash]
	so.node.networkView.mu.RUnlock()

	if !exists || len(storingPeers) == 0 {
		return nil, fmt.Errorf("file %s not found in network", fileHash)
	}

	log.Printf("Found file %s on %d peers", fileHash, len(storingPeers))

	// In a real implementation, we would:
	// 1. Contact peers to retrieve shards
	// 2. Reconstruct file from shards
	// 3. Store locally for future access

	// For now, return error since we don't have the actual shard transfer implemented
	return nil, fmt.Errorf("distributed retrieval not yet implemented for file %s", fileHash)
}

// DeleteFileDistributed deletes a file from the network
func (so *StorageOperations) DeleteFileDistributed(fileHash string) error {
	// Delete from local storage
	if err := so.fileManager.DeleteFile(fileHash); err != nil {
		return fmt.Errorf("failed to delete file locally: %w", err)
	}

	// Announce deletion to network
	so.announceFileDeletion(fileHash)

	log.Printf("File %s deleted and removal announced to network", fileHash)
	return nil
}

// selectStoragePeers selects the best peers for storing data
func (so *StorageOperations) selectStoragePeers(count int) []peer.ID {
	so.node.networkView.mu.RLock()
	defer so.node.networkView.mu.RUnlock()

	var candidates []peer.ID

	// First, try to get high-reputation peers if reputation system is available
	if so.node.reputation != nil {
		bestPeers := so.node.reputation.GetBestPeers(0.7, count)
		if len(bestPeers) >= count {
			return bestPeers[:count]
		}
		candidates = append(candidates, bestPeers...)
	}

	// If not enough high-reputation peers, include all suitable peers
	for peerID, peerInfo := range so.node.networkView.peers {
		if len(candidates) >= count {
			break
		}

		// Skip if already in candidates
		alreadySelected := false
		for _, candidate := range candidates {
			if candidate == peerID {
				alreadySelected = true
				break
			}
		}
		if alreadySelected {
			continue
		}

		// Check if peer is suitable for storage
		// Use safe checks in case peerInfo or Info is nil
		isSuitable := true

		if peerInfo == nil {
			isSuitable = false
		} else if peerInfo.Info != nil {
			// Check if peer is active and has sufficient storage
			if !peerInfo.Info.IsActive ||
				(peerInfo.Info.StorageCapacity-peerInfo.Info.UsedStorage) <= 100*1024*1024 {
				isSuitable = false
			}
		} else {
			// If no Info, use reliability score
			if peerInfo.Reliability < 0.3 {
				isSuitable = false
			}
		}

		if isSuitable {
			candidates = append(candidates, peerID)
		}
	}

	// If still not enough peers, just return what we have
	log.Printf("Selected %d storage peers out of requested %d", len(candidates), count)
	return candidates
}

// announceFileToNetwork announces file storage to the network
func (so *StorageOperations) announceFileToNetwork(fileHash string, storagePeers []peer.ID) {
	fileInfo, err := so.fileManager.GetFileInfo(fileHash)
	if err != nil {
		log.Printf("Failed to get file info for announcement: %v", err)
		return
	}

	// Create file announcement
	fileAnnounce := types.FileAnnounceMessage{
		FileHash: fileHash,
		Locations: []types.FileLocation{
			{
				FileHash:    fileHash,
				ShardHashes: fileInfo.ShardHashes,
				PeerIDs:     storagePeers,
				Timestamp:   time.Now(),
			},
		},
		Timestamp: time.Now(),
	}

	payload, err := fileAnnounce.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal file announcement: %v", err)
		return
	}

	msg := types.GossipMessage{
		Type:      types.GossipTypeFileAnnounce,
		PeerID:    so.node.node.Host.ID(),
		Timestamp: time.Now(),
		Payload:   payload,
	}

	if err := so.node.gossip.publishMessage(msg); err != nil {
		log.Printf("Failed to announce file to network: %v", err)
	} else {
		log.Printf("File %s announced to network", fileHash)
	}
}

// announceFileDeletion announces file deletion to the network
func (so *StorageOperations) announceFileDeletion(fileHash string) {
	deleteMsg := types.FileDeleteMessage{
		FileHash:  fileHash,
		Timestamp: time.Now(),
		PeerID:    so.node.node.Host.ID(),
	}

	payload, err := deleteMsg.ToJSON()
	if err != nil {
		log.Printf("Failed to marshal file deletion message: %v", err)
		return
	}

	msg := types.GossipMessage{
		Type:      types.GossipTypeFileDelete,
		PeerID:    so.node.node.Host.ID(),
		Timestamp: time.Now(),
		Payload:   payload,
	}

	if err := so.node.gossip.publishMessage(msg); err != nil {
		log.Printf("Failed to announce file deletion: %v", err)
	} else {
		log.Printf("File %s deletion announced to network", fileHash)
	}
}

// GetStorageStats returns storage operation statistics
func (so *StorageOperations) GetStorageStats() map[string]interface{} {
	so.mu.RLock()
	defer so.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["pending_operations"] = len(so.pendingOps)

	var completedOps, failedOps int
	for _, op := range so.pendingOps {
		if op.Status == "completed" {
			completedOps++
		} else if op.Status == "failed" {
			failedOps++
		}
	}

	stats["completed_operations"] = completedOps
	stats["failed_operations"] = failedOps

	// Add file manager stats
	fileStats := so.fileManager.GetFileStats()
	for k, v := range fileStats {
		stats[k] = v
	}

	return stats
}

// MonitorStorageOperations monitors and cleans up completed operations
func (so *StorageOperations) MonitorStorageOperations() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-so.node.ctx.Done():
			return
		case <-ticker.C:
			so.cleanupCompletedOperations()
		}
	}
}

// cleanupCompletedOperations removes old completed operations
func (so *StorageOperations) cleanupCompletedOperations() {
	so.mu.Lock()
	defer so.mu.Unlock()

	cleanupThreshold := time.Now().Add(-1 * time.Hour)
	for fileHash, operation := range so.pendingOps {
		if (operation.Status == "completed" || operation.Status == "failed") &&
			operation.CompletedAt.Before(cleanupThreshold) {
			delete(so.pendingOps, fileHash)
			log.Printf("Cleaned up completed operation for file %s", fileHash)
		}
	}
}
