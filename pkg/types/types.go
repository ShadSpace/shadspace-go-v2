package types

import (
	"encoding/json"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// MasterNodeInterface defines the interface that API can use without creating import cycles
type MasterNodeInterface interface {
	GetHost() host.Host
	GetMetrics() *NetworkMetrics
	GetFarmers() []*FarmerInfo
}

// FarmerInfo contains comprehensive information about a farmer node
type FarmerInfo struct {
	// Basic identification
	PeerID    peer.ID  `json:"peer_id"`
	NodeName  string   `json:"node_name"`
	Version   string   `json:"version"`
	Addresses []string `json:"addresses"`

	// Storage capacity
	StorageCapacity uint64 `json:"storage_capacity"`
	UsedStorage     uint64 `json:"used_storage"`

	// Performance metrics
	Reliability float64 `json:"reliability"`
	Latency     int     `json:"latency"` // ms
	Uptime      float64 `json:"uptime"`  // seconds or percentage

	// Status and location
	Location  string   `json:"location"`
	IsActive  bool     `json:"is_active"`
	IsPrimary bool     `json:"is_primary,omitempty"`
	Tags      []string `json:"tags"`

	// Timestamps
	LastSeen  time.Time `json:"last_seen"`
	StartTime time.Time `json:"start_time,omitempty"`
	LastSync  time.Time `json:"last_sync,omitempty"`
}

// NetworkMetrics tracks network-wide metrics
type NetworkMetrics struct {
	TotalFarmers   int    `json:"total_farmers"`
	ActiveFarmers  int    `json:"active_farmers"`
	TotalStorage   uint64 `json:"total_storage"`
	UsedStorage    uint64 `json:"used_storage"`
	FilesStored    int    `json:"files_stored"`
	UploadsToday   int    `json:"uploads_today"`
	DownloadsToday int    `json:"downloads_today"`
}

// PerformanceMetrics for detailed farmer performance tracking
type PerformanceMetrics struct {
	Reliability   float64 `json:"reliability"`
	ResponseTime  int64   `json:"response_time_ms"`
	SuccessRate   float64 `json:"success_rate"`
	StorageHealth float64 `json:"storage_health"`
	ChunksStored  int     `json:"chunks_stored"`
}

// Message types
const (
	TypeRegistrationRequest  = "registration_request"
	TypeRegistrationResponse = "registration_response"
	TypeProofOfStorage       = "proof_of_storage"
	TypeFileLocationQuery    = "file_location_query"
	TypeFileLocationResponse = "file_location_response"
	TypeStorageOffer         = "storage_offer"
	TypeChunkRequest         = "chunk_request"
	TypeChunkResponse        = "chunk_response"
	TypeStorageChallenge     = "storage_challenge"
	TypeChallengeResponse    = "challenge_response"
)

// Base message structure
type Message struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	PeerID    peer.ID   `json:"peer_id"`
	Success   bool      `json:"success,omitempty"`
}

// Registration messages
type RegistrationRequest struct {
	Message
	StorageCapacity uint64   `json:"storage_capacity"`
	UsedStorage     uint64   `json:"used_storage"`
	Addresses       []string `json:"addresses"`
	ProtocolVersion string   `json:"protocol_version"`
	Location        string   `json:"location,omitempty"`
	NodeName        string   `json:"node_name,omitempty"`
	Version         string   `json:"version,omitempty"`
}

type RegistrationResponse struct {
	Message
	StatusMessage string `json:"message"`
	AssignedID    string `json:"assigned_id,omitempty"`
}

// Proof of Storage messages
type ProofOfStorage struct {
	Message
	UsedStorage      uint64             `json:"used_storage"`
	AvailableStorage uint64             `json:"available_storage"`
	ChunksStored     int                `json:"chunks_stored"`
	Uptime           float64            `json:"uptime_seconds"`
	StorageProofs    []StorageProof     `json:"storage_proofs"`
	Performance      PerformanceMetrics `json:"performance"`
	Location         string             `json:"location,omitempty"`
	Latency          int                `json:"latency_ms,omitempty"`
}

type StorageProof struct {
	ChunkHash string    `json:"chunk_hash"`
	Proof     []byte    `json:"proof"`
	Timestamp time.Time `json:"timestamp"`
	Size      int       `json:"size"`
}

// Storage Challenge messages
type StorageChallenge struct {
	Message
	ChallengeID string    `json:"challenge_id"`
	ChunkHashes []string  `json:"chunk_hashes"`
	Timestamp   time.Time `json:"timestamp"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type ChallengeResponse struct {
	Message
	ChallengeID string         `json:"challenge_id"`
	Proofs      []StorageProof `json:"proofs"`
	RespondedAt time.Time      `json:"responded_at"`
}

// File location messages
type FileLocationQuery struct {
	Message
	FileHash   string `json:"file_hash"`
	ChunkIndex int    `json:"chunk_index,omitempty"`
}

type FileLocationResponse struct {
	Message
	FileHash  string          `json:"file_hash"`
	Locations []ChunkLocation `json:"locations"`
}

type ChunkLocation struct {
	ChunkHash string    `json:"chunk_hash"`
	Farmers   []peer.ID `json:"farmers"`
}

// Storage messages
type StorageOffer struct {
	Message
	ChunkHash string `json:"chunk_hash"`
	Size      uint64 `json:"size"`
	Duration  int64  `json:"duration_seconds"`
	Price     uint64 `json:"price,omitempty"`
}

type ChunkRequest struct {
	Message
	ChunkHash string `json:"chunk_hash"`
	Priority  int    `json:"priority,omitempty"`
}

type ChunkResponse struct {
	Message
	ChunkHash string `json:"chunk_hash"`
	Data      []byte `json:"data"`
	Error     string `json:"error,omitempty"`
	Success   bool   `json:"success"`
}

type GossipMessageType string

const (
	GossipTypeNodeInfo         GossipMessageType = "node_info"
	GossipTypeFileAnnounce     GossipMessageType = "file_announce"
	GossipTypeReputationUpdate GossipMessageType = "reputation_update"
	GossipTypeFileDelete       GossipMessageType = "file_delete"
	GossipTypeValidatorUpdate  GossipMessageType = "validator_update"
)

type GossipMessage struct {
	Type      GossipMessageType `json:"type"`
	PeerID    peer.ID           `json:"peer_id"`
	Timestamp time.Time         `json:"timestamp"`
	Payload   json.RawMessage   `json:"payload"`
}

// ToJSON converts FarmerInfo to JSON for gossip
func (fi *FarmerInfo) ToJSON() json.RawMessage {
	data, _ := json.Marshal(fi)
	return data
}

func (fa *FileAnnounceMessage) ToJSON() (json.RawMessage, error) {
	data, err := json.Marshal(fa)
	return json.RawMessage(data), err
}

func (fd *FileDeleteMessage) ToJSON() (json.RawMessage, error) {
	data, err := json.Marshal(fd)
	return json.RawMessage(data), err
}

type FileAnnounceMessage struct {
	FileHash   string         `json:"file_hash"`
	Locations  []FileLocation `json:"locations"`
	Timestamp  time.Time      `json:"timestamp"`
	SourcePeer peer.ID        `json:"source_peer"`
}

type FileDeleteMessage struct {
	FileHash  string    `json:"file_hash"`
	Timestamp time.Time `json:"timestamp"`
	PeerID    peer.ID   `json:"peer_id"`
}

type FileLocation struct {
	FileHash       string    `json:"file_hash"`
	ShardHashes    []string  `json:"shard_hashes,omitempty"`
	PeerIDs        []peer.ID `json:"peer_ids"`
	Timestamp      time.Time `json:"timestamp"`
	FileSize       int64     `json:"file_size"`
	TotalShards    int       `json:"total_shards"`
	RequiredShards int       `json:"required_shards"`
}

type FileMetadataRequest struct {
	FileHash string `json:"file_hash"`
}

type FileMetadataResponse struct {
	Metadata *storage.FileMetadata `json:"metadata,omitempty"`
	Error    string                `json:"error,omitempty"`
}

type FileListRequest struct {
	Since time.Time `json:"since"`
}

type FileListResponse struct {
	FileIndex map[string][]peer.ID `json:"file_index"`
	Error     string               `json:"error,omitempty"`
}

// Shard request/response
type ShardRequest struct {
	FileHash   string `json:"file_hash"`
	ShardIndex int    `json:"shard_index"`
}

type ShardResponse struct {
	Data  []byte `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// ValidatorInfo represents validator information for network transmission
type ValidatorInfo struct {
	PeerID          peer.ID `json:"peer_id"`
	StakeAmount     string  `json:"stake_amount"` // String for big.Int serialization
	Reputation      float64 `json:"reputation"`
	Weight          float64 `json:"weight"`
	IsActive        bool    `json:"is_active"`
	ProofsVerified  uint64  `json:"proofs_verified"`
	Availability    float64 `json:"availability"`
	AvgResponseTime int64   `json:"avg_response_time"`
}

// ValidatorUpdateMessage represents a validator committee update
type ValidatorUpdateMessage struct {
	CycleID    uint64          `json:"cycle_id"`
	Validators []ValidatorInfo `json:"validators"`
	StartTime  time.Time       `json:"start_time"`
	EndTime    time.Time       `json:"end_time"`
	Signature  []byte          `json:"signature"` // For future signing implementation
}

type GatewayInfo struct {
	PeerID         peer.ID   `json:"peer_id"`
	NodeName       string    `json:"node_name"`
	APIPort        int       `json:"api_port"`
	NodePort       int       `json:"node_port"`
	Version        string    `json:"version"`
	IsActive       bool      `json:"is_active"`
	StartTime      time.Time `json:"start_time"`
	LastSeen       time.Time `json:"last_seen"`
	ConnectedPeers int       `json:"connected_peers"`
	TotalRequests  int64     `json:"total_requests"`
	Uptime         string    `json:"uptime,omitempty"`
}

type NodeInfo struct {
	ID              string    `json:"id"`
	NodeType        string    `json:"node_type"` // "gateway", "farmer", "validator"
	IsActive        bool      `json:"is_active"`
	StorageCapacity uint64    `json:"storage_capacity"`
	UsedStorage     uint64    `json:"used_storage"`
	Reliability     float64   `json:"reliability"`
	LastSeen        time.Time `json:"last_seen"`
	Addresses       []string  `json:"addresses,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
}

// StorageRequest represents a request to store or retrieve data
type StorageRequest struct {
	Operation      string    `json:"operation"` // "store_shard", "retrieve_shard"
	FileHash       string    `json:"file_hash"`
	FileName       string    `json:"file_name"`
	ShardIndex     int       `json:"shard_index"`
	ShardData      []byte    `json:"shard_data,omitempty"`
	TotalShards    int       `json:"total_shards"`
	RequiredShards int       `json:"required_shards"`
	Timestamp      time.Time `json:"timestamp"`
}

// StorageResponse represents a response to a storage request
type StorageResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    []byte `json:"data,omitempty"`
}
