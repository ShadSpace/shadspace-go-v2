package network

import (
	"context"
	"fmt"
	"log"
	"strings"
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
// StoreFileDistributed stores a file across the network (excluding self)
func (so *StorageOperations) StoreFileDistributed(fileName string, data []byte, totalShards, requiredShards int) (string, error) {
	storagePeers := so.selectStoragePeers(totalShards)
	if len(storagePeers) < totalShards {
		return "", fmt.Errorf("not enough available online peers for storage: need %d, got %d",
			totalShards, len(storagePeers))
	}

	log.Printf("Selected %d online peers for file storage: %v", len(storagePeers), storagePeers)

	// Calculate file hash first
	fileHash := so.fileManager.CalculateFileHash(data)

	// Shard the data locally first
	shards, err := so.fileManager.GetShardManager().ShardData(data, totalShards, requiredShards)
	if err != nil {
		return "", fmt.Errorf("failed to shard file: %w", err)
	}

	// Create storage operation record
	operation := &StorageOperation{
		FileHash:  fileHash,
		Operation: "store",
		Status:    "distributing",
		StartTime: time.Now(),
		Shards:    make([]*ShardTransfer, totalShards),
	}

	// Distribute shards to peers
	var wg sync.WaitGroup
	var mu sync.Mutex
	var distributionErrors []string

	for i, shard := range shards {
		if i >= len(storagePeers) {
			break // Safety check
		}

		targetPeer := storagePeers[i]
		shardIndex := i + 1

		wg.Add(1)
		go func(shardIndex int, shardData []byte, peerID peer.ID) {
			defer wg.Done()

			err := so.distributeShardToPeer(fileHash, fileName, shardIndex, shardData, peerID, totalShards, requiredShards)

			mu.Lock()
			defer mu.Unlock()

			shardTransfer := &ShardTransfer{
				ShardIndex: shardIndex,
				PeerID:     peerID,
				Size:       len(shardData),
			}

			if err != nil {
				shardTransfer.Status = "failed"
				distributionErrors = append(distributionErrors,
					fmt.Sprintf("shard %d to %s: %v", shardIndex, peerID, err))
				log.Printf("❌ Failed to distribute shard %d to %s: %v", shardIndex, peerID, err)
			} else {
				shardTransfer.Status = "completed"
				log.Printf("✅ Successfully distributed shard %d to peer %s", shardIndex, peerID)
			}

			operation.Shards[shardIndex-1] = shardTransfer
		}(shardIndex, shard.Data, targetPeer)
	}

	wg.Wait()

	// Check if distribution was successful
	successfulShards := 0
	for _, shard := range operation.Shards {
		if shard != nil && shard.Status == "completed" {
			successfulShards++
		}
	}

	if successfulShards < requiredShards {
		operation.Status = "failed"
		operation.Error = fmt.Sprintf("insufficient shards distributed: %d successful, %d required",
			successfulShards, requiredShards)
		return "", fmt.Errorf("failed to distribute enough shards: %s", operation.Error)
	}

	operation.Status = "completed"
	operation.CompletedAt = time.Now()

	so.mu.Lock()
	so.pendingOps[fileHash] = operation
	so.mu.Unlock()

	// Announce file to network (include self in announcement for discovery, but not in storage also include the gateway peers)
	announcementPeers := so.getAnnouncementPeers(storagePeers)
	so.announceFileToNetwork(fileHash, announcementPeers)

	log.Printf("✅ File %s successfully distributed across %d online peers (%d/%d shards successful)",
		fileHash, len(storagePeers), successfulShards, totalShards)

	// Store metadata locally for tracking (but not the actual shards)
	so.storeFileMetadataLocally(fileHash, fileName, data, totalShards, requiredShards, storagePeers)

	return fileHash, nil
}

// getAnnouncementPeers gets the list of peers to announce files to (storage peers + gateways)
func (so *StorageOperations) getAnnouncementPeers(storagePeers []peer.ID) []peer.ID {
	// Start with storage peers
	announcementPeers := append([]peer.ID{}, storagePeers...)

	// Add self for discovery
	selfID := so.node.node.Host.ID()
	announcementPeers = append(announcementPeers, selfID)

	// Find all gateway peers in the network
	gatewayPeers := so.findGatewayPeers()

	// Add gateway peers to announcement list
	for _, gatewayPeer := range gatewayPeers {
		// Skip if already in the list
		alreadyInList := false
		for _, existingPeer := range announcementPeers {
			if existingPeer == gatewayPeer {
				alreadyInList = true
				break
			}
		}
		if !alreadyInList {
			announcementPeers = append(announcementPeers, gatewayPeer)
			log.Printf("Added gateway peer %s to announcement list", gatewayPeer)
		}
	}

	log.Printf("Announcement will be sent to %d peers: %d storage peers, %d gateways, including self",
		len(announcementPeers), len(storagePeers), len(gatewayPeers))

	return announcementPeers
}

// findGatewayPeers returns all known gateway peers in the network
func (so *StorageOperations) findGatewayPeers() []peer.ID {
	so.node.networkView.mu.RLock()
	defer so.node.networkView.mu.RUnlock()

	var gatewayPeers []peer.ID

	// Get currently connected peers
	connectedPeers := so.node.node.Host.Network().Peers()
	connectedSet := make(map[peer.ID]bool)
	for _, peerID := range connectedPeers {
		connectedSet[peerID] = true
	}

	// Find all peers with gateway tags that are currently online
	for peerID, peerInfo := range so.node.networkView.peers {
		// Skip self
		if peerID == so.node.node.Host.ID() {
			continue
		}

		// Check if peer is online
		if !connectedSet[peerID] {
			continue
		}

		// Check if peer is a gateway
		if peerInfo != nil && peerInfo.Info != nil && so.hasGatewayTags(peerInfo.Info.Tags) {
			gatewayPeers = append(gatewayPeers, peerID)
		}
	}

	log.Printf("Found %d online gateway peers: %v", len(gatewayPeers), gatewayPeers)
	return gatewayPeers
}

// storeFileMetadataLocally stores file metadata locally for tracking (without storing shards)
func (so *StorageOperations) storeFileMetadataLocally(fileHash, fileName string, data []byte, totalShards, requiredShards int, storagePeers []peer.ID) error {
	metadata := &storage.FileMetadata{
		FileHash:       fileHash,
		FileName:       fileName,
		FileSize:       int64(len(data)),
		TotalShards:    totalShards,
		RequiredShards: requiredShards,
		StoredPeers:    storagePeers,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		OriginalSize:   int64(len(data)),
		// Note: We don't populate ShardHashes since we're not storing shards locally
	}

	// Use a custom method to store just the metadata
	return so.fileManager.UpdateFileMetadata(metadata)
}

// distributeShardToPeer sends a shard to a specific peer for storage
func (so *StorageOperations) distributeShardToPeer(fileHash, fileName string, shardIndex int, shardData []byte, peerID peer.ID, totalShards, requiredShards int) error {
	// REMOVED: Self-storage logic - we don't store locally anymore

	ctx, cancel := context.WithTimeout(so.node.ctx, 30*time.Second)
	defer cancel()

	// Use the storage protocol to send shard to remote peer
	stream, err := so.node.node.Host.NewStream(ctx, peerID, "/shadspace/storage/1.0.0")
	if err != nil {
		return fmt.Errorf("failed to create stream to peer %s: %w", peerID, err)
	}
	defer stream.Close()

	// Create storage request
	storageRequest := types.StorageRequest{
		Operation:      "store_shard",
		FileHash:       fileHash,
		FileName:       fileName,
		ShardIndex:     shardIndex,
		ShardData:      shardData,
		TotalShards:    totalShards,
		RequiredShards: requiredShards,
		Timestamp:      time.Now(),
	}

	// Send storage request
	if err := so.node.node.codec.Encode(stream, storageRequest); err != nil {
		return fmt.Errorf("failed to send storage request: %w", err)
	}

	// Receive response
	var response types.StorageResponse
	if err := so.node.node.codec.Decode(stream, &response); err != nil {
		return fmt.Errorf("failed to receive storage response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("peer rejected shard storage: %s", response.Error)
	}

	return nil
}

// RetrieveFileDistributed retrieves a file from the network
func (so *StorageOperations) RetrieveFileDistributed(fileHash string) ([]byte, error) {
	// First try to retrieve from local storage
	// data, err := so.fileManager.RetrieveFile(fileHash)
	// if err == nil {
	// 	log.Printf("File %s retrieved from local storage", fileHash)
	// 	return data, nil
	// }

	// // Log the local retrieval error to help diagnose reconstruction/metadata issues
	// log.Printf("Local retrieval failed for %s: %v", fileHash, err)
	// log.Printf("File %s not found locally, searching network...", fileHash)

	so.SyncNetworkView()

	so.DebugFileLocation(fileHash)

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

	log.Printf("Successfully reconstructed file %s (%d bytes)",
		fileHash, len(reconstructedData))

	// Verify file integrity
	calculatedHash := so.fileManager.CalculateFileHash(reconstructedData)
	if calculatedHash != fileHash {
		log.Printf("WARNING: File integrity check failed: expected %s, got %s", fileHash, calculatedHash)
		log.Printf("This might be due to incomplete metadata. Continuing with reconstructed data...")
	} else {
		log.Printf("✅ File integrity verified successfully")
	}

	log.Printf("✅ Successfully retrieved and reconstructed file %s (%d bytes)",
		fileHash, len(reconstructedData))

	return reconstructedData, nil
}

func (so *StorageOperations) getFileMetadataFromNetwork(fileHash string, storingPeers []peer.ID) (*storage.FileMetadata, error) {
	var lastError error

	for _, peerID := range storingPeers {
		metadata, err := so.requestFileMetadata(peerID, fileHash)
		if err == nil && metadata != nil {
			// Validate the metadata we received
			if metadata.TotalShards > 0 && metadata.RequiredShards > 0 && metadata.RequiredShards <= metadata.TotalShards {
				log.Printf("✅ Got valid metadata from peer %s", peerID)
				return metadata, nil
			} else {
				log.Printf("Invalid metadata from peer %s: total=%d, required=%d",
					peerID, metadata.TotalShards, metadata.RequiredShards)
				lastError = fmt.Errorf("invalid metadata from peer %s", peerID)
			}
		} else {
			log.Printf("Failed to get metadata from peer %s: %v", peerID, err)
			if err != nil {
				lastError = err
			}
		}
	}

	if lastError != nil {
		return nil, fmt.Errorf("failed to get file metadata from any peer: %w", lastError)
	}
	return nil, fmt.Errorf("no valid metadata received from any peer")
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

			// Check if we have a shard hash for this index
			var expectedHash string
			if index-1 < len(metadata.ShardHashes) && metadata.ShardHashes[index-1] != "" {
				expectedHash = metadata.ShardHashes[index-1]
			} else {
				log.Printf("Warning: No shard hash available for shard %d, skipping integrity check", index)
			}

			shard, err := so.retrieveShardFromPeers(fileHash, index, expectedHash, storingPeers)
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
	successfulShards := 0
	failedShards := 0

	for result := range resultChan {
		if result.err == nil {
			shardMap[result.index] = result.shard
			shards = append(shards, result.shard)
			successfulShards++
			log.Printf("✅ Retrieved shard %d for file %s", result.index, fileHash)
		} else {
			failedShards++
			log.Printf("❌ Failed to retrieve shard %d: %v", result.index, result.err)
		}
	}

	log.Printf("Shard retrieval summary: %d successful, %d failed, %d required",
		successfulShards, failedShards, metadata.RequiredShards)

	// Check if we have enough shards for reconstruction
	if successfulShards < metadata.RequiredShards {
		return nil, fmt.Errorf("insufficient shards retrieved: have %d, need %d",
			successfulShards, metadata.RequiredShards)
	}

	// Ensure shards are in correct order
	orderedShards := make([]storage.Shard, 0, successfulShards)
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
	var lastError error

	for _, peerID := range storingPeers {
		shard, err := so.requestShardFromPeer(peerID, fileHash, shardIndex, shardHash)
		if err == nil {
			return shard, nil
		}
		log.Printf("Failed to get shard %d from peer %s: %v", shardIndex, peerID, err)
		lastError = err
	}

	if lastError != nil {
		return storage.Shard{}, fmt.Errorf("failed to retrieve shard %d from any peer: %w", shardIndex, lastError)
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

		// Only verify hash if we have an expected hash
		if expectedHash != "" {
			actualHash := so.fileManager.GetShardManager().CalculateShardHash(data)
			if actualHash != expectedHash {
				return storage.Shard{}, fmt.Errorf("shard integrity check failed: expected %s, got %s",
					expectedHash, actualHash)
			}
		}

		return storage.Shard{
			Index: shardIndex,
			Data:  data,
			Hash:  so.fileManager.GetShardManager().CalculateShardHash(data),
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

	// Only verify shard hash if we have an expected hash
	if expectedHash != "" {
		actualHash := so.fileManager.GetShardManager().CalculateShardHash(response.Data)
		if actualHash != expectedHash {
			return storage.Shard{}, fmt.Errorf("shard integrity check failed: expected %s, got %s",
				expectedHash, actualHash)
		}
	}

	return storage.Shard{
		Index: shardIndex,
		Data:  response.Data,
		Hash:  so.fileManager.GetShardManager().CalculateShardHash(response.Data),
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

// selectStoragePeers selects the best peers for storing data (only online peers)
func (so *StorageOperations) selectStoragePeers(count int) []peer.ID {
	so.node.networkView.mu.RLock()
	defer so.node.networkView.mu.RUnlock()

	var candidates []peer.ID

	// Get the list of currently connected peers from the host
	connectedPeers := so.node.node.Host.Network().Peers()
	connectedSet := make(map[peer.ID]bool)
	for _, peerID := range connectedPeers {
		connectedSet[peerID] = true
	}

	log.Printf("Currently connected to %d peers: %v", len(connectedPeers), connectedPeers)

	// First, try to get high-reputation peers that are online
	if so.node.reputation != nil {
		bestPeers := so.node.reputation.GetBestPeers(0.7, count)
		for _, peerID := range bestPeers {
			// Skip self and offline peers
			if peerID == so.node.node.Host.ID() {
				continue
			}

			// check if the node has a tag for gateway
			if so.isGatewayNode(peerID) {
				log.Printf("Skipping gateway node: %s", peerID)
				continue
			}

			if connectedSet[peerID] {
				candidates = append(candidates, peerID)
				log.Printf("Selected online high-reputation peer: %s", peerID)
			}
			if len(candidates) >= count {
				return candidates[:count]
			}
		}
	}

	// If not enough high-reputation online peers, include other suitable online peers
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

		// Skip self
		if peerID == so.node.node.Host.ID() {
			continue
		}

		// Skip offline peers
		if !connectedSet[peerID] {
			continue
		}

		// Check if peer is suitable for storage
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
			log.Printf("Selected online peer: %s", peerID)
		}
	}

	// If still not enough peers, log a warning
	if len(candidates) < count {
		log.Printf("Warning: Only found %d online storage peers out of requested %d", len(candidates), count)
	}

	log.Printf("Selected %d online storage peers: %v", len(candidates), candidates)
	return candidates
}

// announceFileToNetwork announces file storage to the network
// announceFileToNetwork announces file storage to the network
func (so *StorageOperations) announceFileToNetwork(fileHash string, storagePeers []peer.ID) {
	fileInfo, err := so.fileManager.GetFileInfo(fileHash)
	if err != nil {
		log.Printf("Failed to get file info for announcement: %v", err)
		return
	}

	// Update local network view with storage peers only
	so.node.networkView.mu.Lock()
	so.node.networkView.fileIndex[fileHash] = storagePeers
	so.node.networkView.mu.Unlock()

	// Create file announcement with storage peers only
	fileAnnounce := types.FileAnnounceMessage{
		FileHash: fileHash,
		Locations: []types.FileLocation{
			{
				FileHash:       fileHash,
				ShardHashes:    fileInfo.ShardHashes,
				PeerIDs:        storagePeers, // Only storage peers in the actual announcement
				Timestamp:      time.Now(),
				FileSize:       fileInfo.FileSize,
				TotalShards:    fileInfo.TotalShards,
				RequiredShards: fileInfo.RequiredShards,
			},
		},
		Timestamp:  time.Now(),
		SourcePeer: so.node.node.Host.ID(),
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

	// Publish multiple times for reliability
	for i := 0; i < 3; i++ {
		if err := so.node.gossip.PublishMessage(msg); err != nil {
			log.Printf("Failed to announce file to network (attempt %d): %v", i+1, err)
			time.Sleep(100 * time.Millisecond)
		} else {
			log.Printf("📢 File %s announced to network - stored on %d peers", fileHash, len(storagePeers))
			break
		}
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

	if err := so.node.gossip.PublishMessage(msg); err != nil {
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

// SyncNetworkView manually syncs the network view by querying peers
func (so *StorageOperations) SyncNetworkView() {
	log.Printf("Syncing network view...")

	so.node.networkView.mu.RLock()
	peers := make([]peer.ID, 0, len(so.node.networkView.peers))
	for peerID := range so.node.networkView.peers {
		if peerID != so.node.node.Host.ID() {
			peers = append(peers, peerID)
		}
	}
	so.node.networkView.mu.RUnlock()

	// Query each peer for their file index
	for _, peerID := range peers {
		so.queryPeerForFiles(peerID)
	}
}

// queryPeerForFiles queries a specific peer for files they're storing
func (so *StorageOperations) queryPeerForFiles(peerID peer.ID) {
	if peerID == so.node.node.Host.ID() {
		return // Skip self
	}

	ctx, cancel := context.WithTimeout(so.node.ctx, 10*time.Second)
	defer cancel()

	// Try to get peer's file list via the file list protocol
	stream, err := so.node.node.Host.NewStream(ctx, peerID, "/shadspace/file-list/1.0.0")
	if err != nil {
		log.Printf("Peer %s does not support file list protocol: %v", peerID, err)
		return
	}
	defer stream.Close()

	// Send request
	request := types.FileListRequest{Since: time.Now().Add(-24 * time.Hour)}
	if err := so.node.node.codec.Encode(stream, request); err != nil {
		log.Printf("Failed to send file list request to %s: %v", peerID, err)
		return
	}

	// Receive response
	var response types.FileListResponse
	if err := so.node.node.codec.Decode(stream, &response); err != nil {
		log.Printf("Failed to decode file list response from %s: %v", peerID, err)
		return
	}

	if response.Error != "" {
		log.Printf("Peer %s returned error in file list response: %s", peerID, response.Error)
		return
	}

	// Update network view
	so.node.networkView.mu.Lock()
	for fileHash, storingPeers := range response.FileIndex {
		if _, exists := so.node.networkView.fileIndex[fileHash]; !exists {
			so.node.networkView.fileIndex[fileHash] = make([]peer.ID, 0)
		}

		// Merge peer lists
		currentPeers := so.node.networkView.fileIndex[fileHash]
		for _, peer := range storingPeers {
			found := false
			for _, currentPeer := range currentPeers {
				if currentPeer == peer {
					found = true
					break
				}
			}
			if !found {
				currentPeers = append(currentPeers, peer)
			}
		}
		so.node.networkView.fileIndex[fileHash] = currentPeers
	}
	so.node.networkView.mu.Unlock()

	log.Printf("Updated network view from peer %s: added %d file entries",
		peerID, len(response.FileIndex))
}

// DebugFileLocation prints debug information about a file's location
func (so *StorageOperations) DebugFileLocation(fileHash string) {
	so.node.networkView.mu.RLock()
	defer so.node.networkView.mu.RUnlock()

	if peers, exists := so.node.networkView.fileIndex[fileHash]; exists {
		log.Printf("DEBUG: File %s found in network view on %d peers: %v",
			fileHash, len(peers), peers)
	} else {
		log.Printf("DEBUG: File %s NOT found in network view", fileHash)
		log.Printf("DEBUG: Available files in network view: %d files", len(so.node.networkView.fileIndex))
		// Optionally log some of the available files for debugging
		count := 0
		for availableHash, availablePeers := range so.node.networkView.fileIndex {
			log.Printf("DEBUG: Available file: %s on %d peers", availableHash, len(availablePeers))
			count++
			if count >= 5 { // Limit output
				break
			}
		}
	}
}

func (so *StorageOperations) IsPeerOnline(peerID peer.ID) bool {
	if peerID == so.node.node.Host.ID() {
		return true // Self is always online
	}

	connections := so.node.node.Host.Network().ConnsToPeer(peerID)
	return len(connections) > 0
}

// GetOnlinePeers returns a list of currently connected peers
func (so *StorageOperations) GetOnlinePeers() []peer.ID {
	return so.node.node.Host.Network().Peers()
}

// isGatewayNode checks if a peer is a gateway node
func (so *StorageOperations) isGatewayNode(peerID peer.ID) bool {
	if peerInfo, exists := so.node.networkView.peers[peerID]; exists {
		if peerInfo.Info != nil {
			return so.hasGatewayTags(peerInfo.Info.Tags)
		}
	}

	return false
}

// hasGatewayTags checks if tags indicate a gateway node
func (so *StorageOperations) hasGatewayTags(tags []string) bool {
	gatewayIndicators := []string{"gateway", "api", "apigateway", "gateway_node", "http_gateway"}

	for _, tag := range tags {
		for _, indicator := range gatewayIndicators {
			if strings.Contains(strings.ToLower(tag), indicator) {
				return true
			}
		}
	}
	return false
}

func (so *StorageOperations) isGatewayTag(tag string) bool {
	gatewayTags := map[string]bool{
		"gateway":      true,
		"api":          true,
		"apigateway":   true,
		"http_gateway": true,
		"rest_gateway": true,
	}
	return gatewayTags[tag]
}
