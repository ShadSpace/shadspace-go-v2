package network

import (
	"context"
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

	// Log the local retrieval error to help diagnose reconstruction/metadata issues
	log.Printf("Local retrieval failed for %s: %v", fileHash, err)
	log.Printf("File %s not found locally, searching network...", fileHash)

	// Look for file in network view
	so.node.networkView.mu.RLock()
	storingPeers, exists := so.node.networkView.fileIndex[fileHash]
	so.node.networkView.mu.RUnlock()

	if !exists || len(storingPeers) == 0 {
		return nil, fmt.Errorf("file %s not found in network", fileHash)
	}

	log.Printf("Found file %s on %d peers: %v", fileHash, len(storingPeers), storingPeers)

	// Get file metadata to understand sharding requirements
	fileMetadata, err := so.getFileMetadataFromNetwork(fileHash, storingPeers)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	log.Printf("File requires %d shards, %d needed for reconstruction",
		fileMetadata.TotalShards, fileMetadata.RequiredShards)

	// Retrieve shards from peers
	shards, err := so.retrieveShardsFromPeers(fileHash, fileMetadata, storingPeers)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve shards: %w", err)
	}

	log.Printf("Successfully retrieved %d/%d shards for file %s",
		len(shards), fileMetadata.TotalShards, fileHash)

	// Reconstruct the file
	reconstructedData, err := so.fileManager.GetShardManager().ReconstructData(shards, fileMetadata.TotalShards, fileMetadata.RequiredShards, int(fileMetadata.OriginalSize))
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct file: %w", err)
	}

	// Verify file integrity
	// calculatedHash := so.fileManager.CalculateFileHash(reconstructedData)
	// if calculatedHash != fileHash {
	// 	return nil, fmt.Errorf("file integrity check failed: expected %s, got %s",
	// 		fileHash, calculatedHash)
	// }

	// Store locally for future access
	if _, err := so.fileManager.StoreFile(
		fileMetadata.FileName,
		reconstructedData,
		fileMetadata.TotalShards,
		fileMetadata.RequiredShards,
		[]peer.ID{so.node.node.Host.ID()}, // Store locally
	); err != nil {
		log.Printf("Warning: failed to cache file locally: %v", err)
	}

	log.Printf("✅ Successfully retrieved and reconstructed file %s (%d bytes)",
		fileHash, len(reconstructedData))

	return reconstructedData, nil
}

func (so *StorageOperations) getFileMetadataFromNetwork(fileHash string, storingPeers []peer.ID) (*storage.FileMetadata, error) {
	for _, peerID := range storingPeers {
		metadata, err := so.requestFileMetadata(peerID, fileHash)
		if err == nil {
			return metadata, nil
		}
		log.Printf("Failed to get metadata from peer %s: %v", peerID, err)
	}
	return nil, fmt.Errorf("failed to get file metadata from any peer")
}

// requestFileMetadata requests file metadata from a specific peer
func (so *StorageOperations) requestFileMetadata(peerID peer.ID, fileHash string) (*storage.FileMetadata, error) {
	// If the peerID is this node, return local metadata directly (avoid dialing self)
	if peerID == so.node.node.Host.ID() {
		meta, err := so.fileManager.GetFileInfo(fileHash)
		if err != nil {
			return nil, fmt.Errorf("local metadata not found: %w", err)
		}
		return meta, nil
	}

	ctx, cancel := context.WithTimeout(so.node.ctx, 10*time.Second)
	defer cancel()

	stream, err := so.node.node.Host.NewStream(ctx, peerID, "/shadspace/file-metadata/1.0.0")
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	// Send request
	request := types.FileMetadataRequest{FileHash: fileHash}
	if err := so.node.node.codec.Encode(stream, request); err != nil {
		return nil, fmt.Errorf("failed to send metadata request: %w", err)
	}

	// Receive response
	var response types.FileMetadataResponse
	if err := so.node.node.codec.Decode(stream, &response); err != nil {
		return nil, fmt.Errorf("failed to receive metadata response: %w", err)
	}

	if response.Error != "" {
		return nil, fmt.Errorf("peer returned error: %s", response.Error)
	}

	return response.Metadata, nil
}

// retrieveShardsFromPeers retrieves shards from multiple peers
func (so *StorageOperations) retrieveShardsFromPeers(fileHash string, metadata *storage.FileMetadata, storingPeers []peer.ID) ([]storage.Shard, error) {
	type shardResult struct {
		shard storage.Shard
		index int
		err   error
	}

	// Create a channel to collect shard results
	resultChan := make(chan shardResult, metadata.TotalShards)
	var wg sync.WaitGroup

	// Try to retrieve each shard
	for shardIndex := 1; shardIndex <= metadata.TotalShards; shardIndex++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			shard, err := so.retrieveShardFromPeers(fileHash, index, metadata.ShardHashes[index-1], storingPeers)
			resultChan <- shardResult{
				shard: shard,
				index: index,
				err:   err,
			}
		}(shardIndex)
	}

	// Wait for all shard retrieval attempts to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect successful shards
	shards := make([]storage.Shard, 0, metadata.TotalShards)
	shardMap := make(map[int]storage.Shard)

	for result := range resultChan {
		if result.err == nil {
			shardMap[result.index] = result.shard
			shards = append(shards, result.shard)
			log.Printf("Retrieved shard %d for file %s", result.index, fileHash)
		} else {
			log.Printf("Failed to retrieve shard %d: %v", result.index, result.err)
		}
	}

	// Check if we have enough shards for reconstruction
	if len(shards) < metadata.RequiredShards {
		return nil, fmt.Errorf("insufficient shards retrieved: have %d, need %d",
			len(shards), metadata.RequiredShards)
	}

	// Ensure shards are in correct order
	orderedShards := make([]storage.Shard, 0, len(shards))
	for i := 1; i <= metadata.TotalShards; i++ {
		if shard, exists := shardMap[i]; exists {
			orderedShards = append(orderedShards, shard)
		}
	}

	return orderedShards, nil
}

// retrieveShardFromPeers attempts to retrieve a specific shard from multiple peers
func (so *StorageOperations) retrieveShardFromPeers(fileHash string, shardIndex int, shardHash string, storingPeers []peer.ID) (storage.Shard, error) {
	// Try peers in order until we succeed
	for _, peerID := range storingPeers {
		shard, err := so.requestShardFromPeer(peerID, fileHash, shardIndex, shardHash)
		if err == nil {
			return shard, nil
		}
		log.Printf("Failed to get shard %d from peer %s: %v", shardIndex, peerID, err)
	}
	return storage.Shard{}, fmt.Errorf("failed to retrieve shard %d from any peer", shardIndex)
}

func (so *StorageOperations) requestShardFromPeer(peerID peer.ID, fileHash string, shardIndex int, expectedHash string) (storage.Shard, error) {
	// If asking self, return local shard directly to avoid self-dial
	if peerID == so.node.node.Host.ID() {
		data, err := so.node.GetShard(fileHash, shardIndex)
		if err != nil {
			return storage.Shard{}, fmt.Errorf("failed to get local shard: %w", err)
		}

		actualHash := so.fileManager.GetShardManager().CalculateShardHash(data)
		if actualHash != expectedHash {
			return storage.Shard{}, fmt.Errorf("shard integrity check failed: expected %s, got %s",
				expectedHash, actualHash)
		}

		return storage.Shard{
			Index: shardIndex,
			Data:  data,
			Hash:  actualHash,
			Size:  len(data),
		}, nil
	}

	ctx, cancel := context.WithTimeout(so.node.ctx, 15*time.Second)
	defer cancel()

	stream, err := so.node.node.Host.NewStream(ctx, peerID, "/shadspace/file-shard/1.0.0")
	if err != nil {
		return storage.Shard{}, fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	// Send shard request
	request := types.ShardRequest{
		FileHash:   fileHash,
		ShardIndex: shardIndex,
	}
	if err := so.node.node.codec.Encode(stream, request); err != nil {
		return storage.Shard{}, fmt.Errorf("failed to send shard request: %w", err)
	}

	// Receive shard response
	var response types.ShardResponse
	if err := so.node.node.codec.Decode(stream, &response); err != nil {
		return storage.Shard{}, fmt.Errorf("failed to receive shard response: %w", err)
	}

	if response.Error != "" {
		return storage.Shard{}, fmt.Errorf("peer returned error: %s", response.Error)
	}

	// Verify shard hash
	actualHash := so.fileManager.GetShardManager().CalculateShardHash(response.Data)
	if actualHash != expectedHash {
		return storage.Shard{}, fmt.Errorf("shard integrity check failed: expected %s, got %s",
			expectedHash, actualHash)
	}

	return storage.Shard{
		Index: shardIndex,
		Data:  response.Data,
		Hash:  actualHash,
		Size:  len(response.Data),
	}, nil
}

// // Add these helper methods to access shard manager
// func (so *StorageOperations) GetShardManager() *storage.ShardManager {
// 	return so.fileManager.ShardManager()
// }

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

	// Update local network view first
	so.node.networkView.mu.Lock()
	so.node.networkView.fileIndex[fileHash] = storagePeers
	so.node.networkView.mu.Unlock()

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
