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
}

// FarmerInfo contains information about a farmer node
type FarmerInfo struct {
	PeerID          peer.ID
	StorageCapacity uint64
	UsedStorage     uint64
	Reliability     float64
	LastSeen        time.Time
	IsActive        bool
	Addresses       []string
}

// NetworkMetrics tracks network-wide metrics
type NetworkMetrics struct {
	TotalFarmers   int
	ActiveFarmers  int
	TotalStorage   uint64
	UsedStorage    uint64
	FilesStored    int
	UploadsToday   int
	DownloadsToday int
}