package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// FileManager handle file stored and retrieval operations
type FileManager struct {
	engine       *Engine
	shardManager *ShardManager
	mu           sync.RWMutex
	fileIndex    map[string]*FileMetadata
}

// FileMetadata contains information about stored files
type FileMetadata struct {
	FileHash       string
	FileName       string
	FileSize       int64
	TotalShards    int
	RequiredShards int
	ShardHashes    []string
	StoredPeers    []peer.ID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	MimeType       string
}

// NewFileManager creates a new file manager
func NewFileManager(engine *Engine, shardManager *ShardManager) *FileManager {
	return &FileManager{
		engine:       engine,
		shardManager: shardManager,
		fileIndex:    make(map[string]*FileMetadata),
	}
}

// StoreFIle stores a file by sharding it and distributing shards
func (fm *FileManager) StoreFile(fileName string, data []byte, totalShards, requiredShards int, storagePeers []peer.ID) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("cannot store empty file")
	}

	if requiredShards > totalShards {
		return "", fmt.Errorf("required shards cannot exceed total shards")
	}

	if len(storagePeers) < totalShards {
		return "", fmt.Errorf("not enough storage peers: need %d, got %d", totalShards, len(storagePeers))
	}

	// Calculate file has
	fileHash := fm.CalculateFileHash(data)

	fm.mu.Lock()
	defer fm.mu.Unlock()

	// Check if file already exists
	if _, exists := fm.fileIndex[fileHash]; exists {
		return fileHash, fmt.Errorf("file already exists with hash: %s", fileHash)
	}

	// Store file metadata
	metadata := &FileMetadata{
		FileHash:       fileHash,
		FileName:       fileName,
		FileSize:       int64(len(data)),
		TotalShards:    totalShards,
		RequiredShards: requiredShards,
		StoredPeers:    storagePeers[:totalShards],
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// For now, we'll store all shards locally for testing
	shards, err := fm.shardManager.ShardData(data, totalShards, requiredShards)
	if err != nil {
		return "", fmt.Errorf("failed to shard file: %w", err)
	}

	// Store each shard locally
	for i, shard := range shards {
		shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
		if err := fm.engine.StoreChunk(shardID, shard.Data); err != nil {
			// Cleanup already stored shards on error
			for j := 0; j < i; j++ {
				cleanupID := fmt.Sprintf("%s_shard_%d", fileHash, j+1)
				fm.engine.DeleteChunk(cleanupID)
			}
			return "", fmt.Errorf("failed to store shard %d: %w", i+1, err)
		}
		metadata.ShardHashes = append(metadata.ShardHashes, shard.Hash)
	}

	fm.fileIndex[fileHash] = metadata

	log.Printf("File %s stored successfully with %d shards (%d required)",
		fileHash, totalShards, requiredShards)

	return fileHash, nil
}

// RetrieveFile retrieves and reconstructs a file
func (fm *FileManager) RetrieveFile(fileHash string) ([]byte, error) {
	fm.mu.RLock()
	metadata, exists := fm.fileIndex[fileHash]
	fm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("file not found: %s", fileHash)
	}

	// Retrieve shards from local storage
	shards := make([]Shard, 0, metadata.TotalShards)
	shardsRetrieved := 0

	for i := 0; i < metadata.TotalShards; i++ {
		shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
		shardData, err := fm.engine.RetrieveChunk(shardID)
		if err != nil {
			log.Printf("Failed to retrieve shard %d: %v", i+1, err)
			continue
		}

		shard := Shard{
			Index: i + 1,
			Data:  shardData,
			Hash:  metadata.ShardHashes[i],
			Size:  len(shardData),
		}
		shards = append(shards, shard)
		shardsRetrieved++
	}

	// Check if we have enough shards for reconstruction
	if shardsRetrieved < metadata.RequiredShards {
		return nil, fmt.Errorf("insufficient shards: have %d, need %d",
			shardsRetrieved, metadata.RequiredShards)
	}

	log.Printf("Retrieved %d/%d shards for file %s", shardsRetrieved, metadata.TotalShards, fileHash)

	// Reconstruct the file
	data, err := fm.shardManager.ReconstructData(shards)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct file: %w", err)
	}

	// Verify the reconstructed data matches the original hash
	reconstructedHash := fm.CalculateFileHash(data)
	if reconstructedHash != fileHash {
		return nil, fmt.Errorf("file integrity check failed: expected %s, got %s",
			fileHash, reconstructedHash)
	}

	log.Printf("File %s reconstructed successfully (%d bytes)", fileHash, len(data))
	return data, nil
}

// DeleteFile deletes a file and all its shards
func (fm *FileManager) DeleteFile(fileHash string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	metadata, exists := fm.fileIndex[fileHash]
	if !exists {
		return fmt.Errorf("file not found: %s", fileHash)
	}

	// Delete all shards
	for i := 0; i < metadata.TotalShards; i++ {
		shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
		if err := fm.engine.DeleteChunk(shardID); err != nil {
			log.Printf("Warning: failed to delete shard %s: %v", shardID, err)
		}
	}

	// Remove from file index
	delete(fm.fileIndex, fileHash)

	log.Printf("File %s deleted successfully", fileHash)
	return nil
}

// GetFileInfo returns metadata for a file
func (fm *FileManager) GetFileInfo(fileHash string) (*FileMetadata, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	metadata, exists := fm.fileIndex[fileHash]
	if !exists {
		return nil, fmt.Errorf("file not found: %s", fileHash)
	}

	return metadata, nil
}

// ListFiles returns a list of all stored files
func (fm *FileManager) ListFiles() []*FileMetadata {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	files := make([]*FileMetadata, 0, len(fm.fileIndex))
	for _, metadata := range fm.fileIndex {
		files = append(files, metadata)
	}

	return files
}

// GetFileStats returns statistics about stored files
func (fm *FileManager) GetFileStats() map[string]interface{} {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_files"] = len(fm.fileIndex)

	var totalSize int64
	for _, metadata := range fm.fileIndex {
		totalSize += metadata.FileSize
	}
	stats["total_file_size"] = totalSize
	stats["average_file_size"] = float64(totalSize) / float64(len(fm.fileIndex))

	return stats
}

// calculateFileHash calculates SHA256 hash of file data
func (fm *FileManager) CalculateFileHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// VerifyFileIntegrity verifies that all shards are present and valid
func (fm *FileManager) VerifyFileIntegrity(fileHash string) (bool, error) {
	metadata, err := fm.GetFileInfo(fileHash)
	if err != nil {
		return false, err
	}

	// Check if all shards are present and valid
	missingShards := 0
	for i := 0; i < metadata.TotalShards; i++ {
		shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
		shardData, err := fm.engine.RetrieveChunk(shardID)
		if err != nil {
			missingShards++
			continue
		}

		// Verify shard hash
		calculatedHash := fm.shardManager.calculateShardHash(shardData)
		if calculatedHash != metadata.ShardHashes[i] {
			return false, fmt.Errorf("shard %d integrity check failed", i+1)
		}
	}

	if missingShards > 0 {
		log.Printf("File %s has %d missing shards", fileHash, missingShards)
		return false, nil
	}

	return true, nil
}
