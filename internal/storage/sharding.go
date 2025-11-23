package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"

	"github.com/ShadSpace/shadspace-go-v2/internal/shamir"
)

// ShardManager manages data sharding and distribution
type ShardManager struct {
	mu           sync.RWMutex
	shardMap     map[string]*ShardMetadata // fileHash -> shard metadata
	chunkToShard map[string]string         // chunkID -> fileHash
}

// ShardMetadata contains information about file shards
type ShardMetadata struct {
	FileHash       string
	TotalShards    int
	RequiredShards int
	ShardSize      int
	ShardHashes    []string
	FarmerPeers    []string // Which farmers store which shards
}

// NewShardManager creates a new shard manager
func NewShardManager() *ShardManager {
	return &ShardManager{
		shardMap:     make(map[string]*ShardMetadata),
		chunkToShard: make(map[string]string),
	}
}

// ShardData splits data into shards using Shamir's Secret Sharing
func (sm *ShardManager) ShardData(data []byte, totalShards, requiredShards int) ([]Shard, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if requiredShards > totalShards {
		return nil, fmt.Errorf("required shards cannot exceed total shards")
	}

	// Split data using Shamir's Secret Sharing
	shares, err := shamir.Split(data, totalShards, requiredShards)
	if err != nil {
		return nil, fmt.Errorf("failed to split data: %w", err)
	}

	// Create shards from shares
	shards := make([]Shard, totalShards)
	for i, share := range shares {
		shardHash := sm.calculateShardHash(share)
		shards[i] = Shard{
			Index: i + 1,
			Data:  share,
			Hash:  shardHash,
			Size:  len(share),
		}
	}

	return shards, nil
}

// ReconstructData reconstructs original data from shards
func (sm *ShardManager) ReconstructData(shards []Shard) ([]byte, error) {
	if len(shards) == 0 {
		return nil, fmt.Errorf("no shards provided")
	}

	// Extract share data from shards
	shares := make([][]byte, len(shards))
	for i, shard := range shards {
		shares[i] = shard.Data
	}

	// Reconstruct using Shamir's Secret Sharing
	data, err := shamir.Combine(shares)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct data: %w", err)
	}

	return data, nil
}

// StoreShardedFile stores a file by sharding it and distributing to farmers
func (sm *ShardManager) StoreShardedFile(fileHash string, data []byte, totalShards, requiredShards int, farmerPeers []string) (*ShardMetadata, error) {
	// Shard the data
	shards, err := sm.ShardData(data, totalShards, requiredShards)
	if err != nil {
		return nil, err
	}

	// Store shard metadata
	shardHashes := make([]string, len(shards))
	for i, shard := range shards {
		shardHashes[i] = shard.Hash
		sm.chunkToShard[shard.Hash] = fileHash
	}

	metadata := &ShardMetadata{
		FileHash:       fileHash,
		TotalShards:    totalShards,
		RequiredShards: requiredShards,
		ShardSize:      len(data) / requiredShards,
		ShardHashes:    shardHashes,
		FarmerPeers:    farmerPeers[:totalShards], // Assign farmers to shards
	}

	sm.mu.Lock()
	sm.shardMap[fileHash] = metadata
	sm.mu.Unlock()

	log.Printf("File %s sharded into %d shards (%d required for reconstruction)",
		fileHash, totalShards, requiredShards)

	return metadata, nil
}

// GetShardMetadata retrieves shard metadata for a file
func (sm *ShardManager) GetShardMetadata(fileHash string) (*ShardMetadata, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	metadata, exists := sm.shardMap[fileHash]
	return metadata, exists
}

// calculateShardHash calculates SHA256 hash of shard data
func (sm *ShardManager) calculateShardHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Shard represents a single data shard
type Shard struct {
	Index int
	Data  []byte
	Hash  string
	Size  int
}
