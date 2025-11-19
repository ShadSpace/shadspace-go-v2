package types

import (
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// MasterNodeInterface defines the interface that API can use without creating import cycles
type MasterNodeInterface interface {
	GetHost() host.Host
	GetMetrics() *NetworkMetrics
	GetFarmers() []*FarmerInfo
	GetEnhancedFarmers() []*FarmerEnhancedMetrics
}

type FarmerEnhancedMetrics struct {
	Location  string    `json:"location"`
	NodeName  string    `json:"node_name"`
	Version   string    `json:"version"`
	Latency   int       `json:"latency"`
	Uptime    float64   `json:"uptime"`
	IsPrimary bool      `json:"is_primary"`
	Tags      []string  `json:"tags"`
	LastSync  time.Time `json:"last_sync"`
}

// FarmerInfo contains information about a farmer node
type FarmerInfo struct {
	PeerID          peer.ID   `json:"peer_id"`
	StorageCapacity uint64    `json:"storage_capacity"`
	UsedStorage     uint64    `json:"used_storage"`
	Reliability     float64   `json:"reliability"`
	LastSeen        time.Time `json:"last_seen"`
	IsActive        bool      `json:"is_active"`
	Addresses       []string  `json:"addresses"`
	Location        string    `json:"location,omitempty"`
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
	StatusMessage string `json:"message"` // Changed from Message to StatusMessage
	AssignedID    string `json:"assigned_id,omitempty"`
}

// Proof of Storage messages
type ProofOfStorage struct {
	Message
	UsedStorage      uint64         `json:"used_storage"`
	AvailableStorage uint64         `json:"available_storage"`
	ChunksStored     int            `json:"chunks_stored"`
	Uptime           float64        `json:"uptime_seconds"`
	StorageProofs    []StorageProof `json:"storage_proofs"`
	Metrics          FarmerMetrics  `json:"metrics"`
	Location         string         `json:"location,omitempty"`
	Latency          int            `json:"latency_ms,omitempty"`
}

type StorageProof struct {
	ChunkHash string    `json:"chunk_hash"`
	Proof     []byte    `json:"proof"`
	Timestamp time.Time `json:"timestamp"`
	Size      int       `json:"size"`
}

type FarmerMetrics struct {
	Reliability   float64 `json:"reliability"`
	ResponseTime  int64   `json:"response_time_ms"`
	SuccessRate   float64 `json:"success_rate"`
	StorageHealth float64 `json:"storage_health"`
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
	Size      int    `json:"size"`
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
	Success   bool
}
