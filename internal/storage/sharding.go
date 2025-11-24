package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"

	"github.com/klauspost/reedsolomon"
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

// ShardData splits data into shards using Reed–Solomon erasure coding.
// totalShards = dataShards + parityShards. requiredShards == dataShards.
func (sm *ShardManager) ShardData(data []byte, totalShards, requiredShards int) ([]Shard, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if requiredShards > totalShards {
		return nil, fmt.Errorf("required shards cannot exceed total shards")
	}
	if requiredShards <= 0 {
		return nil, fmt.Errorf("required shards must be > 0")
	}

	dataShards := requiredShards
	parityShards := totalShards - dataShards
	if parityShards < 0 {
		return nil, fmt.Errorf("invalid shard configuration")
	}

	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create reedsolomon encoder: %w", err)
	}

	// Split the data into equal sized slices (encoder will pad last piece)
	shards, err := enc.Split(data)
	if err != nil {
		return nil, fmt.Errorf("failed to split data: %w", err)
	}

	// Encode parity
	if err := enc.Encode(shards); err != nil {
		return nil, fmt.Errorf("failed to encode parity shards: %w", err)
	}

	// Wrap shards into our Shard type with hashes
	out := make([]Shard, len(shards))
	for i := range shards {
		shardHash := sm.CalculateShardHash(shards[i])
		out[i] = Shard{
			Index: i + 1,
			Data:  shards[i],
			Hash:  shardHash,
			Size:  len(shards[i]),
		}
	}

	return out, nil
}

// ReconstructData reconstructs original data from shards using Reed–Solomon.
func (sm *ShardManager) ReconstructData(shards []Shard, totalShards, requiredShards, outputSize int) ([]byte, error) {
	if len(shards) == 0 {
		return nil, fmt.Errorf("no shards provided")
	}
	if requiredShards > totalShards {
		return nil, fmt.Errorf("required shards cannot exceed total shards")
	}

	dataShards := requiredShards
	parityShards := totalShards - dataShards

	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create reedsolomon encoder: %w", err)
	}

	// Build [][]byte array for encoder
	raw := make([][]byte, totalShards)
	present := 0

	for i := 0; i < totalShards; i++ {
		if i < len(shards) && len(shards[i].Data) > 0 {
			raw[i] = shards[i].Data
			present++
		} else {
			raw[i] = nil
		}
	}

	if present < dataShards {
		return nil, fmt.Errorf("insufficient shards to reconstruct: have %d, need %d", present, dataShards)
	}

	// Reconstruct missing shards
	if err := enc.Reconstruct(raw); err != nil {
		return nil, fmt.Errorf("reedsolomon reconstruct failed: %w", err)
	}

	// Use a bytes.Buffer as the io.Writer for Join with exact output size
	var buf bytes.Buffer
	err = enc.Join(&buf, raw, outputSize)
	if err != nil {
		return nil, fmt.Errorf("reedsolomon join failed: %w", err)
	}

	return buf.Bytes(), nil
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
	var shardSize int
	if len(shards) > 0 {
		shardSize = shards[0].Size // Use the Size field from Shard struct
	}

	for i, shard := range shards {
		shardHashes[i] = shard.Hash
		sm.chunkToShard[shard.Hash] = fileHash
	}

	metadata := &ShardMetadata{
		FileHash:       fileHash,
		TotalShards:    totalShards,
		RequiredShards: requiredShards,
		ShardSize:      shardSize,
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

// CalculateShardHash calculates SHA256 hash of shard data
func (sm *ShardManager) CalculateShardHash(data []byte) string {
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
