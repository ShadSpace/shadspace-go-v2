package storage

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Config holds storage engine configuration
type Config struct {
	MaxStorageBytes uint64
}

// Engine handles file chunk storage and retrieval
type Engine struct {
	mu           sync.RWMutex
	chunks       map[string]string
	config       Config
	storageDir   string
	metadataFile string
}

// NewEngine creates a new storage engine
func NewEngine(storageDir string) (*Engine, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	engine := &Engine{
		chunks:       make(map[string]string),
		storageDir:   storageDir,
		metadataFile: filepath.Join(storageDir, "metadata.gob"),
		config: Config{
			MaxStorageBytes: 10 * 1024 * 1024 * 1024, // 10GB default
		},
	}

	// Load existing metadata
	if err := engine.loadMetadata(); err != nil {
		log.Printf("Warning: Could not load existing metadata: %v", err)
	}

	return engine, nil
}

// NewEngineWithConfig creates a new storage engine with custom configuration
func NewEngineWithConfig(storageDir string, config Config) (*Engine, error) {
	engine, err := NewEngine(storageDir)
	if err != nil {
		return nil, err
	}

	engine.config = config
	return engine, nil

}

// StoreChunk stores a file chunk
func (e *Engine) StoreChunk(chunkID string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.chunks[chunkID]; exists {
		return fmt.Errorf("chunk %s already exists", chunkID)
	}

	// Check if we have enough storage space
	newUsedBytes := e.calculateUsedBytes() + uint64(len(data))
	if newUsedBytes > e.config.MaxStorageBytes {
		return fmt.Errorf("insufficient storage space: %d bytes needed, %d available",
			len(data), e.config.MaxStorageBytes-e.calculateUsedBytes())
	}

	// create chunk file path
	chunkPath := filepath.Join(e.storageDir, chunkID+".chunk")

	// Write data to disk
	if err := os.WriteFile(chunkPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write chunk to disk: %w", err)
	}

	// Update metadata
	e.chunks[chunkID] = chunkPath

	// Persist metadata
	if err := e.saveMetadata(); err != nil {
		// Clean up the file if metadata save fails
		os.Remove(chunkPath)
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	log.Printf("Stored chunk %s to disk (%d bytes)", chunkID, len(data))
	return nil
}

// RetrieveChunk retrieves a file chunk from disk
func (e *Engine) RetrieveChunk(chunkID string) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	chunkPath, exists := e.chunks[chunkID]
	if !exists {
		return nil, fmt.Errorf("chunk %s not found", chunkID)
	}

	data, err := os.ReadFile(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk from disk: %w", err)
	}

	return data, nil
}

// DeleteChunk deletes a file chunk from disk
func (e *Engine) DeleteChunk(chunkID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	chunkPath, exists := e.chunks[chunkID]
	if !exists {
		return fmt.Errorf("chunk %s not found", chunkID)
	}

	// Delete the file
	if err := os.Remove(chunkPath); err != nil {
		return fmt.Errorf("failed to delete chunk file: %w", err)
	}

	// Update metadata
	delete(e.chunks, chunkID)

	// Persist metadata
	if err := e.saveMetadata(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	log.Printf("Deleted chunk %s from disk", chunkID)
	return nil
}

// GetChunkCount returns the number of stored chunks
func (e *Engine) GetChunkCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.chunks)
}

// calculateUsedBytes calculates total bytes used by all stored chunks
func (e *Engine) calculateUsedBytes() uint64 {
	var totalBytes uint64
	for _, chunkPath := range e.chunks {
		if info, err := os.Stat(chunkPath); err == nil {
			totalBytes += uint64(info.Size())
		}
	}
	return totalBytes
}

// calculateUsagePercentage calculates storage usage percentage
func (e *Engine) calculateUsagePercentage() float64 {
	if e.config.MaxStorageBytes == 0 {
		return 0
	}
	return (float64(e.calculateUsedBytes()) / float64(e.config.MaxStorageBytes)) * 100
}

// GetStats returns storage engine statistics
func (e *Engine) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	usedBytes := e.calculateUsedBytes()
	usagePercentage := e.calculateUsagePercentage()

	stats := make(map[string]interface{})
	stats["total_chunks"] = len(e.chunks)
	stats["used_bytes"] = usedBytes
	stats["total_capacity"] = e.config.MaxStorageBytes
	stats["usage_percentage"] = usagePercentage
	stats["available_bytes"] = e.config.MaxStorageBytes - usedBytes
	stats["storage_directory"] = e.storageDir

	return stats
}

// GetConfig returns the storage engine configuration
func (e *Engine) GetConfig() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// SetConfig updates the storage engine configuration
func (e *Engine) SetConfig(config Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

// ListChunks returns a list of all stored chunk IDs
func (e *Engine) ListChunks() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	chunkIDs := make([]string, 0, len(e.chunks))
	for chunkID := range e.chunks {
		chunkIDs = append(chunkIDs, chunkID)
	}
	return chunkIDs
}

// GetChunkSize returns the size of a specific chunk in bytes
func (e *Engine) GetChunkSize(chunkID string) (int, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	chunkPath, exists := e.chunks[chunkID]
	if !exists {
		return 0, fmt.Errorf("chunk %s not found", chunkID)
	}

	info, err := os.Stat(chunkPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get chunk size: %w", err)
	}

	return int(info.Size()), nil
}

// loadMetadata loads chunk metadata from disk
func (e *Engine) loadMetadata() error {
	file, err := os.Open(e.metadataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No metadata file exists yet
		}
		return err
	}
	defer file.Close()

	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&e.chunks); err != nil {
		return fmt.Errorf("failed to decode metadata: %w", err)
	}

	// Verify that all chunk files still exist
	for chunkID, chunkPath := range e.chunks {
		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			log.Printf("Warning: Chunk file missing, removing from index: %s", chunkID)
			delete(e.chunks, chunkID)
		}
	}

	log.Printf("Loaded metadata for %d chunks", len(e.chunks))
	return nil
}

// saveMetadata saves chunk metadata to disk
func (e *Engine) saveMetadata() error {
	file, err := os.Create(e.metadataFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(e.chunks); err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}

	return nil
}

// Cleanup removes any orphaned chunk files
func (e *Engine) Cleanup() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Read all files in storage directory
	files, err := os.ReadDir(e.storageDir)
	if err != nil {
		return fmt.Errorf("failed to read storage directory: %w", err)
	}

	removedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filename := file.Name()
		// Skip metadata file
		if filename == "metadata.gob" {
			continue
		}

		// Check if this file is in our index
		found := false
		for _, chunkPath := range e.chunks {
			if filepath.Base(chunkPath) == filename {
				found = true
				break
			}
		}

		if !found {
			// This is an orphaned file, remove it
			orphanPath := filepath.Join(e.storageDir, filename)
			if err := os.Remove(orphanPath); err != nil {
				log.Printf("Warning: Failed to remove orphaned file %s: %v", orphanPath, err)
			} else {
				removedCount++
				log.Printf("Removed orphaned file: %s", filename)
			}
		}
	}

	log.Printf("Cleanup removed %d orphaned files", removedCount)
	return nil
}
