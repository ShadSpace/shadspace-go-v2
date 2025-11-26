package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/protocol"
	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// NodeConfig holds configuration for creating a libp2p node
type NodeConfig struct {
	Host           string
	Port           int
	ProtocolID     string
	Rendezvous     string
	NodeType       string
	BootstrapPeers []string
	PrivKey        crypto.PrivKey
	KeyFile        string
}

// Node represents a base libp2p node with DHT and PubSub capabilities
type Node struct {
	Host      host.Host
	DHT       *dht.IpfsDHT
	PubSub    *pubsub.PubSub
	Config    NodeConfig
	ctx       context.Context
	handler   *protocol.MessageHandler
	codec     *codec
	fileStore FileStore // Interface for file storage operations
	mu        sync.RWMutex
}

// FileStore defines the interface for file storage operations needed by protocol handlers
type FileStore interface {
	GetFileMetadata(fileHash string) (*storage.FileMetadata, error)
	GetShard(fileHash string, shardIndex int) ([]byte, error)
	GetFileStats() map[string]interface{}
}

// NewNode creates a new libp2p node with the given configuration
func NewNode(ctx context.Context, config NodeConfig) (*Node, error) {
	// Set up libp2p options
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/%s/tcp/%d", config.Host, config.Port)),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
	}

	// Add private key if provided
	if config.PrivKey != nil {
		opts = append(opts, libp2p.Identity(config.PrivKey))
	} else {
		// If no key provided, generate a temporary one (for backward compatibility)
		log.Println("Warning: No private key provided, generating temporary key")
		priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
		if err != nil {
			return nil, fmt.Errorf("failed to generate temporary key: %w", err)
		}
		opts = append(opts, libp2p.Identity(priv))
	}

	// Create the libp2p host
	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	// Create the DHT
	kadDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	// Create PubSub
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		h.Close()
		kadDHT.Close()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	node := &Node{
		Host:    h,
		DHT:     kadDHT,
		PubSub:  ps,
		Config:  config,
		ctx:     ctx,
		handler: protocol.NewMessageHandler(nil),
		codec:   &codec{},
	}

	// Set up stream handlers
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/bitswap"), node.handleBitSwapStream)
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/storage"), node.handleStorageStream)
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/discovery"), node.handleDiscoveryStream)
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/proofofstorage"), node.handleProofOfStorageStream)

	// Add file retrieval protocol handlers
	h.SetStreamHandler("/shadspace/file-metadata/1.0.0", node.handleFileMetadataRequest)
	h.SetStreamHandler("/shadspace/file-shard/1.0.0", node.handleShardRequest)
	h.SetStreamHandler("/shardspace/proof/1.0.0", node.handleProofVerificationStream)
	h.SetStreamHandler("/shadspace/file-list/1.0.0", node.handleFileListRequest)
	h.SetStreamHandler("/shadspace/storage/1.0.0", node.handleStorageProtocol)

	// Set connection handlers
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF:    node.onPeerConnected,
		DisconnectedF: node.onPeerDisconnected,
	})

	// Bootstrap the DHT
	if err := node.bootstrap(); err != nil {
		log.Printf("Warning: DHT bootstrap failed: %v", err)
	}

	log.Printf("Node %s started successfully", h.ID())
	log.Printf("Addresses: %v", h.Addrs())

	return node, nil
}

// SetFileStore sets the file storage interface for protocol handlers
func (n *Node) SetFileStore(store FileStore) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fileStore = store
}

// Handle file metadata requests
func (n *Node) handleFileMetadataRequest(stream network.Stream) {
	defer stream.Close()

	var request types.FileMetadataRequest
	if err := n.codec.Decode(stream, &request); err != nil {
		log.Printf("Failed to decode metadata request: %v", err)
		return
	}

	log.Printf("Received file metadata request for: %s", request.FileHash)

	response := types.FileMetadataResponse{}

	n.mu.RLock()
	fileStore := n.fileStore
	n.mu.RUnlock()

	if fileStore == nil {
		response.Error = "file storage not available"
	} else {
		metadata, err := fileStore.GetFileMetadata(request.FileHash)
		if err != nil {
			response.Error = fmt.Sprintf("failed to get file metadata: %v", err)
		} else {
			response.Metadata = metadata
			log.Printf("Sending file metadata for: %s", request.FileHash)
		}
	}

	if err := n.codec.Encode(stream, response); err != nil {
		log.Printf("Failed to send metadata response: %v", err)
	}
}

// Handle shard requests
func (n *Node) handleShardRequest(stream network.Stream) {
	defer stream.Close()

	var request types.ShardRequest
	if err := n.codec.Decode(stream, &request); err != nil {
		log.Printf("Failed to decode shard request: %v", err)
		return
	}

	log.Printf("Received shard request for file %s, shard %d", request.FileHash, request.ShardIndex)

	response := types.ShardResponse{}

	n.mu.RLock()
	fileStore := n.fileStore
	n.mu.RUnlock()

	if fileStore == nil {
		response.Error = "file storage not available"
	} else {
		shardData, err := fileStore.GetShard(request.FileHash, request.ShardIndex)
		if err != nil {
			response.Error = fmt.Sprintf("failed to get shard: %v", err)
		} else {
			response.Data = shardData
			log.Printf("Sending shard %d for file %s (%d bytes)",
				request.ShardIndex, request.FileHash, len(shardData))
		}
	}

	if err := n.codec.Encode(stream, response); err != nil {
		log.Printf("Failed to send shard response: %v", err)
	}
}

func (n *Node) handleProofVerificationStream(s network.Stream) {
	defer s.Close()
	log.Printf("Received proof verification stream from %s", s.Conn().RemotePeer())
	// The actual processing will be done by the registered stream handler
	// in the ProofVerifier, which uses the same protocol ID
}

func (n *Node) handleFileListRequest(stream network.Stream) {
	defer stream.Close()

	// Decode request
	var request types.FileListRequest
	if err := n.codec.Decode(stream, &request); err != nil {
		log.Printf("Failed to decode file list request: %v", err)
		return
	}

	log.Printf("Received file list request from %s", stream.Conn().RemotePeer())

	// Build response
	response := types.FileListResponse{
		FileIndex: make(map[string][]peer.ID),
	}

	n.mu.RLock()
	fileStore := n.fileStore
	n.mu.RUnlock()

	if fileStore == nil {
		response.Error = "file storage not available"
	} else {
		// Try to cast fileStore to get access to file list
		if decentralizedNode, ok := fileStore.(*DecentralizedNode); ok {
			localFiles := decentralizedNode.GetFileManager().ListFiles()

			decentralizedNode.networkView.mu.RLock()
			for _, file := range localFiles {
				response.FileIndex[file.FileHash] = []peer.ID{decentralizedNode.GetPeerID()}
			}
			decentralizedNode.networkView.mu.RUnlock()

			log.Printf("Sending file list with %d files to %s", len(localFiles), stream.Conn().RemotePeer())
		} else {
			response.Error = "file store does not support file listing"
		}
	}

	// Send response
	if err := n.codec.Encode(stream, response); err != nil {
		log.Printf("Failed to send file list response: %v", err)
	}
}

func (n *Node) handleStorageProtocol(stream network.Stream) {
	defer stream.Close()

	var request types.StorageRequest
	if err := n.codec.Decode(stream, &request); err != nil {
		log.Printf("Failed to decode storage request: %v", err)
		return
	}

	log.Printf("Received storage request: %s for file %s shard %d",
		request.Operation, request.FileHash, request.ShardIndex)

	var response types.StorageResponse

	switch request.Operation {
	case "store_shard":
		response = n.handleStoreShard(request, stream.Conn().RemotePeer())
	default:
		response.Error = fmt.Sprintf("unknown operation: %s", request.Operation)
	}

	if err := n.codec.Encode(stream, response); err != nil {
		log.Printf("Failed to send storage response: %v", err)
	}
}

// handleStoreShard stores a shard received from another peer
func (n *Node) handleStoreShard(request types.StorageRequest, remotePeer peer.ID) types.StorageResponse {
	n.mu.RLock()
	fileStore := n.fileStore
	n.mu.RUnlock()

	if fileStore == nil {
		return types.StorageResponse{
			Success: false,
			Error:   "file storage not available",
		}
	}

	// Store the shard
	shardID := fmt.Sprintf("%s_shard_%d", request.FileHash, request.ShardIndex)

	if decentralizedNode, ok := fileStore.(*DecentralizedNode); ok {
		// Store the shard data
		if err := decentralizedNode.storage.StoreChunk(shardID, request.ShardData); err != nil {
			log.Printf("Failed to store shard %s: %v", shardID, err)
			return types.StorageResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to store shard: %v", err),
			}
		}

		// Update file metadata in the file manager
		err := n.updateFileMetadata(decentralizedNode, request, remotePeer)
		if err != nil {
			log.Printf("Warning: failed to update file metadata: %v", err)
			// Continue anyway since shard was stored successfully
		}

		log.Printf("✅ Stored shard %d for file %s from peer %s",
			request.ShardIndex, request.FileHash, remotePeer)

		return types.StorageResponse{
			Success: true,
			Message: "shard stored successfully",
		}
	}

	return types.StorageResponse{
		Success: false,
		Error:   "invalid file store type",
	}
}

// updateFileMetadata updates file metadata when receiving a shard
func (n *Node) updateFileMetadata(decentralizedNode *DecentralizedNode, request types.StorageRequest, remotePeer peer.ID) error {
	// Check if we already have metadata for this file
	existingMetadata, err := decentralizedNode.fileManager.GetFileInfo(request.FileHash)
	if err != nil {
		// Create new file metadata since this is the first shard we're receiving
		metadata := &storage.FileMetadata{
			FileHash:       request.FileHash,
			FileName:       request.FileName,
			FileSize:       int64(len(request.ShardData) * request.TotalShards), // Estimate
			TotalShards:    request.TotalShards,
			RequiredShards: request.RequiredShards,
			ShardHashes:    make([]string, request.TotalShards),
			StoredPeers:    []peer.ID{decentralizedNode.GetPeerID()},
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			OriginalSize:   int64(len(request.ShardData) * request.TotalShards), // Estimate
		}

		// Calculate shard hash for this shard
		shardHash := decentralizedNode.fileManager.GetShardManager().CalculateShardHash(request.ShardData)
		if request.ShardIndex-1 < len(metadata.ShardHashes) {
			metadata.ShardHashes[request.ShardIndex-1] = shardHash
		}

		// Use the public UpdateFileMetadata method
		if err := decentralizedNode.fileManager.UpdateFileMetadata(metadata); err != nil {
			return fmt.Errorf("failed to save metadata: %w", err)
		}

		log.Printf("Created new file metadata for %s with %d total shards", request.FileHash, request.TotalShards)
	} else {
		// Update existing metadata
		existingMetadata.UpdatedAt = time.Now()

		// Calculate shard hash for this shard
		shardHash := decentralizedNode.fileManager.GetShardManager().CalculateShardHash(request.ShardData)
		if request.ShardIndex-1 < len(existingMetadata.ShardHashes) {
			existingMetadata.ShardHashes[request.ShardIndex-1] = shardHash
		}

		// Update the stored peers list if not already present
		peerExists := false
		for _, p := range existingMetadata.StoredPeers {
			if p == decentralizedNode.GetPeerID() {
				peerExists = true
				break
			}
		}
		if !peerExists {
			existingMetadata.StoredPeers = append(existingMetadata.StoredPeers, decentralizedNode.GetPeerID())
		}

		// Use the public UpdateFileMetadata method
		if err := decentralizedNode.fileManager.UpdateFileMetadata(existingMetadata); err != nil {
			return fmt.Errorf("failed to save updated metadata: %w", err)
		}

		log.Printf("Updated file metadata for %s", request.FileHash)
	}

	// Update network view
	decentralizedNode.networkView.mu.Lock()
	defer decentralizedNode.networkView.mu.Unlock()

	if _, exists := decentralizedNode.networkView.fileIndex[request.FileHash]; !exists {
		decentralizedNode.networkView.fileIndex[request.FileHash] = []peer.ID{decentralizedNode.GetPeerID()}
	} else {
		// Add this peer to the existing file entry if not already present
		peers := decentralizedNode.networkView.fileIndex[request.FileHash]
		found := false
		for _, peer := range peers {
			if peer == decentralizedNode.GetPeerID() {
				found = true
				break
			}
		}
		if !found {
			decentralizedNode.networkView.fileIndex[request.FileHash] = append(peers, decentralizedNode.GetPeerID())
		}
	}

	return nil
}

// bootstrap connects the node to bootstrap peers and initializes the DHT
func (n *Node) bootstrap() error {
	// Bootstrap the DHT
	if err := n.DHT.Bootstrap(n.ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	// Connect to bootstrap peers if provided
	for _, peerAddr := range n.Config.BootstrapPeers {
		ma, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			log.Printf("Invalid multiaddress %s: %v", peerAddr, err)
			continue
		}

		addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("Failed to parse peer info from %s: %v", peerAddr, err)
			continue
		}

		// Try to connect with timeout
		connectCtx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		defer cancel()

		if err := n.Host.Connect(connectCtx, *addrInfo); err != nil {
			log.Printf("Failed to connect to bootstrap peer %s: %v", peerAddr, err)
			continue
		}

		log.Printf("Connected to bootstrap peer: %s", addrInfo.ID)
	}

	return nil
}

// handleBitSwapStream handles bitswap protocol streams
func (n *Node) handleBitSwapStream(s network.Stream) {
	defer s.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(s).Decode(&raw); err != nil {
		// If decoder fails, fall back to ReadAll to preserve behavior for non-JSON payloads
		data, err2 := io.ReadAll(s)
		if err2 != nil {
			log.Printf("Error reading bitswap stream: %v", err2)
			return
		}
		if err := n.handler.HandleBitSwapMessage(s, data); err != nil {
			log.Printf("Error handling bitswap message: %v", err)
		}
		return
	}

	if err := n.handler.HandleBitSwapMessage(s, raw); err != nil {
		log.Printf("Error handling bitswap message: %v", err)
	}
}

// handleStorageStream handles storage-related protocol streams
func (n *Node) handleStorageStream(s network.Stream) {
	defer s.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(s).Decode(&raw); err != nil {
		data, err2 := io.ReadAll(s)
		if err2 != nil {
			log.Printf("Error reading storage stream: %v", err2)
			return
		}
		if err := n.handler.HandleStorageMessage(s, data); err != nil {
			log.Printf("Error handling storage message: %v", err)
		}
		return
	}

	if err := n.handler.HandleStorageMessage(s, raw); err != nil {
		log.Printf("Error handling storage message: %v", err)
	}

}

// handleDiscoveryStream handles peer discovery and metadata exchange
func (n *Node) handleDiscoveryStream(s network.Stream) {
	defer s.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(s).Decode(&raw); err != nil {
		data, err2 := io.ReadAll(s)
		if err2 != nil {
			log.Printf("Error reading discovery stream: %v", err2)
			return
		}
		if err := n.handler.HandleDiscoveryMessage(s, data); err != nil {
			log.Printf("Error handling discovery message: %v", err)
		}
		return
	}

	if err := n.handler.HandleDiscoveryMessage(s, raw); err != nil {
		log.Printf("Error handling discovery message: %v", err)
	}
}

// handleProofOfStorageStream handles proof of storage protocol streams
func (n *Node) handleProofOfStorageStream(s network.Stream) {
	defer s.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(s).Decode(&raw); err != nil {
		data, err2 := io.ReadAll(s)
		if err2 != nil {
			log.Printf("Error reading proof of storage stream: %v", err2)
			return
		}
		if err := n.handler.HandleProofOfStorageMessage(s, data); err != nil {
			log.Printf("Error handling proof of storage message: %v", err)
		}
		return
	}

	if err := n.handler.HandleProofOfStorageMessage(s, raw); err != nil {
		log.Printf("Error handling proof of storage message: %v", err)
	}
}

// onPeerConnected handles new peer connections
func (n *Node) onPeerConnected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	log.Printf("Connected to peer: %s", peerID)

	// TODO: Add peer to peer store and update metrics
}

// onPeerDisconnected handles peer disconnections
func (n *Node) onPeerDisconnected(net network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	log.Printf("Disconnected from peer: %s", peerID)

	// TODO: Remove peer from active lists and update metrics
}

// SetHandler allows setting the message handler after node creation
func (n *Node) SetHandler(handler *protocol.MessageHandler) {
	n.handler = handler
}

// GetCodec returns the codec instance for stream communication
func (n *Node) GetCodec() *codec {
	return n.codec
}

// Close gracefully shuts down the node
func (n *Node) Close() error {
	var errs []error

	if n.DHT != nil {
		if err := n.DHT.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if n.Host != nil {
		if err := n.Host.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}

	return nil
}

// codec provides JSON encoding/decoding for stream communication
type codec struct{}

func (c *codec) Encode(w io.Writer, v interface{}) error {
	return json.NewEncoder(w).Encode(v)
}

func (c *codec) Decode(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
