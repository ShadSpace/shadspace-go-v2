package interfaces

import (
	"context"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

// ConsensusNode defines the interface that consensus needs from network
type ConsensusNode interface {
	// Basic node information
	GetPeerID() peer.ID
	GetHostID() string
	Ctx() context.Context

	// Network view and discovery
	NetworkView() NetworkView
	GetNetworkStats() map[string]interface{}
	GetPeerInfo(peer.ID) (*PeerInfo, bool)

	// Reputation system
	GetReputation() ReputationSystem

	// Gossip system
	GossipManager() GossipManager

	// Storage operations
	GetFileManager() FileManager
	GetStorageEngine() StorageEngine
	GetShardManager() ShardManager
	GetStorageOperations() StorageOperations

	// Node operations
	Node() Node
	Close() error
}

// NetworkView interface
type NetworkView interface {
	GetPeers() map[peer.ID]*PeerInfo
	WithReadLock(func())
	WithWriteLock(func())
}

// PeerInfo represents information about a peer
type PeerInfo struct {
	Info        *types.FarmerInfo
	LastSeen    time.Time
	Reliability float64
	Distance    int
}

// ReputationSystem interface
type ReputationSystem interface {
	GetPeerScore(peer.ID) float64
	UpdatePeer(peer.ID, float64)
	GetPeerReliability(peer.ID) float64
	GetBestPeers(minScore float64, maxPeers int) []peer.ID
	RecordSuccessfulInteraction(peer.ID)
	RecordFailedInteraction(peer.ID)
	MonitorPeers()
}

// GossipManager interface
type GossipManager interface {
	PublishMessage(interface{}) error
	Start() error
}

// FileManager interface
type FileManager interface {
	GetFileInfo(fileHash string) (*storage.FileMetadata, error)
	ListFiles() []*storage.FileMetadata
	StoreFile(fileName string, data []byte, totalShards, requiredShards int, storagePeers []peer.ID) (string, error)
	RetrieveFile(fileHash string) ([]byte, error)
	DeleteFile(fileHash string) error
	CalculateFileHash(data []byte) string
	VerifyFileIntegrity(fileHash string) (bool, error)
	GetFileStats() map[string]interface{}
	Close() error
}

// StorageEngine interface
type StorageEngine interface {
	StoreChunk(chunkID string, data []byte) error
	RetrieveChunk(chunkID string) ([]byte, error)
	DeleteChunk(chunkID string) error
	GetStats() map[string]interface{}
	Cleanup() error
}

// ShardManager interface
type ShardManager interface {
	ShardData(data []byte, totalShards, requiredShards int) ([]storage.Shard, error)
	ReconstructData(shards []storage.Shard, totalShards, requiredShards int, originalSize int) ([]byte, error)
	CalculateShardHash(data []byte) string
}

// StorageOperations interface
type StorageOperations interface {
	StoreFile(fileName string, data []byte, totalShards, requiredShards int) (string, error)
	RetrieveFile(fileHash string) ([]byte, error)
	DeleteFile(fileHash string) error
	ReplicateFile(fileHash string, targetPeers []peer.ID) error
	MonitorStorageOperations()
}

// Node interface for base libp2p node operations
type Node interface {
	GetCodec() Codec
	Close() error
}

// Codec interface for stream communication
type Codec interface {
	Encode(w interface{}, v interface{}) error
	Decode(r interface{}, v interface{}) error
}

// ... rest of your interfaces remain the same
