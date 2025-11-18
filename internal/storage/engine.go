package storage

import (
	"fmt"
	"sync"
)

// Engine handles file chunk storage and retrieval
type Engine struct {
	mu     sync.RWMutex
	chunks map[string][]byte
}

// NewEngine creates a new storage engine
func NewEngine() (*Engine, error) {
	return &Engine{
		chunks: make(map[string][]byte),
	}, nil
}

// StoreChunk stores a file chunk
func (e *Engine) StoreChunk(chunkID string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.chunks[chunkID]; exists {
		return fmt.Errorf("chunk %s already exists", chunkID)
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