package storage

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	metadataFile string
	storageDir   string
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
	OriginalSize   int64
}

// NewFileManager creates a new file manager
func NewFileManager(storageDir string) (*FileManager, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	engine, err := NewEngine(storageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage engine: %w", err)
	}

	fm := &FileManager{
		engine:       engine,
		shardManager: NewShardManager(),
		fileIndex:    make(map[string]*FileMetadata),
		metadataFile: filepath.Join(storageDir, "file_metadata.gob"),
		storageDir:   storageDir,
	}

	// Load existing file metadata
	if err := fm.loadMetadata(); err != nil {
		log.Printf("Warning: Could not load existing file metadata: %v", err)
	}

	log.Printf("FileManager initialized with storage directory: %s", storageDir)
	return fm, nil
}

// NewFileManagerWithEngine creates a new file manager with an existing engine
func NewFileManagerWithEngine(engine *Engine, shardManager *ShardManager, storageDir string) (*FileManager, error) {
	fm := &FileManager{
		engine:       engine,
		shardManager: shardManager,
		fileIndex:    make(map[string]*FileMetadata),
		metadataFile: filepath.Join(storageDir, "file_metadata.gob"),
		storageDir:   storageDir,
	}

	// Load existing file metadata
	if err := fm.loadMetadata(); err != nil {
		log.Printf("Warning: Could not load existing file metadata: %v", err)
	}

	return fm, nil
}

// StoreFIle stores a file by sharding it and distributing shards
func (fm *FileManager) StoreFile(fileName string, data []byte, totalShards, requiredShards int, storagePeers []peer.ID) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("cannot store empty file")
	}

	if requiredShards > totalShards {
		return "", fmt.Errorf("required shards cannot exceed total shards")
	}

	// Calculate file hash
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
		StoredPeers:    storagePeers,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		OriginalSize:   int64(len(data)),
	}

	// Shard the data
	shards, err := fm.shardManager.ShardData(data, totalShards, requiredShards)
	if err != nil {
		return "", fmt.Errorf("failed to shard file: %w", err)
	}

	// Store each shard to persistent storage
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

	// Update file index
	fm.fileIndex[fileHash] = metadata

	// Save metadata to disk
	if err := fm.saveMetadata(); err != nil {
		// Cleanup shards if metadata save fails
		for i := 0; i < totalShards; i++ {
			cleanupID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
			fm.engine.DeleteChunk(cleanupID)
		}
		delete(fm.fileIndex, fileHash)
		return "", fmt.Errorf("failed to save file metadata: %w", err)
	}

	log.Printf("File %s stored to persistent storage with %d shards (%d required)",
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

	// Retrieve shards from persistent storage
	shards := make([]Shard, 0, metadata.TotalShards)
	shardsRetrieved := 0

	for i := 0; i < metadata.TotalShards; i++ {
		shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
		shardData, err := fm.engine.RetrieveChunk(shardID)
		if err != nil {
			log.Printf("Failed to retrieve shard %d from disk: %v", i+1, err)
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

	log.Printf("Retrieved %d/%d shards for file %s from persistent storage",
		shardsRetrieved, metadata.TotalShards, fileHash)

	// Reconstruct the file
	data, err := fm.shardManager.ReconstructData(shards, metadata.TotalShards, metadata.RequiredShards, int(metadata.OriginalSize))
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct file: %w", err)
	}

	// Verify the reconstructed data matches the original hash
	reconstructedHash := fm.CalculateFileHash(data)
	if reconstructedHash != fileHash {
		return nil, fmt.Errorf("file integrity check failed: expected %s, got %s",
			fileHash, reconstructedHash)
	}

	log.Printf("File %s reconstructed successfully from persistent storage (%d bytes)",
		fileHash, len(data))
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

	// Delete all shards from persistent storage
	for i := 0; i < metadata.TotalShards; i++ {
		shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
		if err := fm.engine.DeleteChunk(shardID); err != nil {
			log.Printf("Warning: failed to delete shard %s from disk: %v", shardID, err)
		}
	}

	// Remove from file index
	delete(fm.fileIndex, fileHash)

	// Save updated metadata to disk
	if err := fm.saveMetadata(); err != nil {
		return fmt.Errorf("failed to save metadata after deletion: %w", err)
	}

	log.Printf("File %s deleted from persistent storage", fileHash)
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

	if len(fm.fileIndex) > 0 {
		stats["average_file_size"] = float64(totalSize) / float64(len(fm.fileIndex))
	} else {
		stats["average_file_size"] = 0.0
	}

	// Add storage engine stats
	engineStats := fm.engine.GetStats()
	for k, v := range engineStats {
		stats["engine_"+k] = v
	}

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
		calculatedHash := fm.shardManager.CalculateShardHash(shardData)
		if calculatedHash != metadata.ShardHashes[i] {
			return false, fmt.Errorf("shard %d integrity check failed", i+1)
		}
	}

	if missingShards > 0 {
		log.Printf("File %s has %d missing shards in persistent storage", fileHash, missingShards)
		return false, nil
	}

	return true, nil
}

// loadMetadata loads file metadata from disk
func (fm *FileManager) loadMetadata() error {
	file, err := os.Open(fm.metadataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No metadata file exists yet
		}
		return err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&fm.fileIndex); err != nil {
		return fmt.Errorf("failed to decode file metadata: %w", err)
	}

	log.Printf("Loaded metadata for %d files from persistent storage", len(fm.fileIndex))
	return nil
}

// saveMetadata saves file metadata to disk
func (fm *FileManager) saveMetadata() error {
	file, err := os.Create(fm.metadataFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(fm.fileIndex); err != nil {
		return fmt.Errorf("failed to encode file metadata: %w", err)
	}

	return nil
}

// Cleanup performs maintenance tasks on persistent storage
func (fm *FileManager) Cleanup() error {
	// Clean up orphaned files in storage engine
	if err := fm.engine.Cleanup(); err != nil {
		return fmt.Errorf("failed to cleanup storage engine: %w", err)
	}

	// Verify all files in index still have their shards
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	corruptedFiles := 0
	for fileHash, metadata := range fm.fileIndex {
		// Check if all shards exist
		allShardsExist := true
		for i := 0; i < metadata.TotalShards; i++ {
			shardID := fmt.Sprintf("%s_shard_%d", fileHash, i+1)
			if _, err := fm.engine.RetrieveChunk(shardID); err != nil {
				allShardsExist = false
				break
			}
		}

		if !allShardsExist {
			log.Printf("File %s has missing shards in persistent storage, marking for cleanup", fileHash)
			corruptedFiles++
			// In production, you might want to handle this differently
			// For now, we'll just log it
		}
	}

	if corruptedFiles > 0 {
		log.Printf("Found %d files with missing shards during persistent storage cleanup", corruptedFiles)
	}

	return nil
}

// GetStorageDirectory returns the storage directory path
func (fm *FileManager) GetStorageDirectory() string {
	return fm.storageDir
}

func (fm *FileManager) GetShardManager() *ShardManager {
	return fm.shardManager
}

// Close performs any necessary cleanup (currently handled by engine)
func (fm *FileManager) Close() error {
	// The engine handles its own cleanup
	// We could add additional cleanup here if needed
	return nil
}
