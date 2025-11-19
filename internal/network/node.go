package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/protocol"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pprotocol "github.com/libp2p/go-libp2p/core/protocol" // Renamed import
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
	Host    host.Host
	DHT     *dht.IpfsDHT
	PubSub  *pubsub.PubSub
	Config  NodeConfig
	ctx     context.Context
	handler *protocol.MessageHandler
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
	}

	// Set up stream handlers
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/bitswap"), node.handleBitSwapStream)
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/storage"), node.handleStorageStream)
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/discovery"), node.handleDiscoveryStream)
	h.SetStreamHandler(libp2pprotocol.ID(config.ProtocolID+"/proofofstorage"), node.handleProofOfStorageStream)

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
