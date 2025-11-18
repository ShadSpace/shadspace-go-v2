package network

import (
	"context"
	"fmt"
	"log"
	// "time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	// "github.com/libp2p/go-libp2p/core/network"
	// "github.com/libp2p/go-libp2p/core/peer"
	// "github.com/libp2p/go-libp2p/core/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	// "github.com/multiformats/go-multiaddr"
)

// NodeConfig holds configuration for creating a libp2p node
type NodeConfig struct {
	Host    	    string
	Port    	    int
	ProtocolID     string
	Rendezvous	    string
	NodeType  	    string
	BootstrapPeers  []string
	PrivKey 		crypto.PrivKey
}

// Node represents a base libp2p node with DHT and PubSub capabilites
type Node struct {
	Host       host.Host
	DHT        *dht.IpfsDHT
	PubSub	   *pubsub.PubSub
	Config	   NodeConfig
	ctx		   context.Context
}

// NewNode creates a new libp2p node with the given configurations
func NewNode (ctx context.Context, config NodeConfig) (*Node, error) {
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
		Host:     h,
		DHT:      kadDHT,
		PubSub:   ps, 
		Config:   config,
		ctx:      ctx,
	}

	// // Set up stream handlers
	// h.SetStreamHandler(protocol.ID(config.ProtocolID+"/bitswap"), node.handleBitSwapStream)
	// h.SetStreamHandler(protocol.ID(config.ProtocolID+"/storage"), node.handleStorageStream)
	// h.SetStreamHandler(protocol.ID(config.ProtocolID+"/discovery"), node.handleDiscoveryStream)

	// // Set connection handlers
	// h.Network().Notify(&network.NotifyBundle{
	// 	ConnectedF:    node.onPeerConnected,
	// 	DisconnectedF: node.onPeerDisconnected,
	// })

	// Bootstrap the DHT
	// if err := node.bootstrap(); err != nil {
	// 	log.Printf("Warning: DHT bootstrap failed: %v", err)
	// }


	log.Printf("Node %d started successfully", h.ID())
	log.Printf("Addresses: %v", h.Addrs())

	return node, nil
}

// TODO: bootrap connect the node to peers and initializes the DHT
// func (n *Node) bootstrap() error {}


// TODO: handleBitSwapStream handles bitswap protocol streams
// func (n *Node) handleBitSwapStream(s network.Stream) {}


// TODO:  handleStorageStream handles storage-related protocol streams
// func (n *Node) handleStorageStream(s network.Stream) {}


// TODO:  handleDiscoveryStream handles peer discovery and metadata exchange
// func (n *Node) handleDiscoveryStream(s network.Stream) {}


// TODO:  onPeerConnected handles new peer connections
// func (n *Node) onPeerConnected(net network.Network, conn network.Conn) {}


// TODO:  onPeerDisconnected handles peer disconnections
// func (n *Node) onPeerDisconnected(net network.Network, conn network.Conn) {}


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