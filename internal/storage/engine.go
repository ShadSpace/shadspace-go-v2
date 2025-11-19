package storage

import (
	"fmt"
	"sync"
)

// Config holds storage engine configuration
type Config struct {
	MaxStorageBytes uint64
}

// Engine handles file chunk storage and retrieval
type Engine struct {
	mu     sync.RWMutex
	chunks map[string][]byte
	config Config
}

// NewEngine creates a new storage engine
func NewEngine() (*Engine, error) {
	return &Engine{
		chunks: make(map[string][]byte),
		config: Config{
			MaxStorageBytes: 10 * 1024 * 1024 * 1024, // 10GB default
		},
	}, nil
}

// NewEngineWithConfig creates a new storage engine with custom configuration
func NewEngineWithConfig(config Config) (*Engine, error) {
	return &Engine{
		chunks: make(map[string][]byte),
		config: config,
	}, nil
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

	e.chunks[chunkID] = data
	return nil
}

// RetrieveChunk retrieves a file chunk
func (e *Engine) RetrieveChunk(chunkID string) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	data, exists := e.chunks[chunkID]
	if !exists {
		return nil, fmt.Errorf("chunk %s not found", chunkID)
	}

	return data, nil
}

// DeleteChunk deletes a file chunk
func (e *Engine) DeleteChunk(chunkID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.chunks[chunkID]; !exists {
		return fmt.Errorf("chunk %s not found", chunkID)
	}

	delete(e.chunks, chunkID)
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
	for _, chunk := range e.chunks {
		totalBytes += uint64(len(chunk))
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

	data, exists := e.chunks[chunkID]
	if !exists {
		return 0, fmt.Errorf("chunk %s not found", chunkID)
	}

	return len(data), nil
}
