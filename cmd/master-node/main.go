package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ShadSpace/shadspace-go-v2/internal/network"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
)

func main() {
	// Parse command-line flags
	keyFile := flag.String("key", "", "Path to private key file")
	port := flag.Int("port", 4003, "Port to listen on")
	host := flag.String("host", "0.0.0.0", "Host address to bind to")
	bootstrap := flag.String("bootstrap", "", "Bootstrap peer address (optional)")
	flag.Parse()

	// If no key file provided, use default
	if *keyFile == "" {
		defaultKeyFile := filepath.Join("keys", "master_node_key.key")
		if err := os.MkdirAll(filepath.Dir(defaultKeyFile), 0700); err != nil {
			log.Printf("Warning: could not create keys directory: %v", err)
			defaultKeyFile = "master_node_key.key"
		}
		*keyFile = defaultKeyFile
	}

	// Create context that cancels on interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load or generate private key
	privKey, err := utils.GetOrCreateKey(*keyFile)
	if err != nil {
		log.Fatalf("Failed to load or generate private key: %v", err)
	}

	peerID, err := utils.GetPeerIDFromKey(privKey)
	if err != nil {
		log.Fatalf("Failed to get peer ID from private key: %v", err)
	}

	log.Printf("Node Using Peer ID: %s", peerID)
	log.Printf("Using key file: %s", *keyFile)

	// Prepare bootstrap peers
	var bootstrapPeers []string
	if *bootstrap != "" {
		bootstrapPeers = []string{*bootstrap}
		log.Printf("Using bootstrap peer: %s", *bootstrap)
	}

	// Load configuration
	config := network.NodeConfig{
		Host:           *host,
		Port:           *port,
		ProtocolID:     "/shadspace/1.0.0",
		Rendezvous:     "shadspace-network",
		NodeType:       "master",
		BootstrapPeers: bootstrapPeers,
		PrivKey:        privKey,
		KeyFile:        *keyFile,
	}

	// Create and start the master node - FIXED FUNCTION NAME
	masterNode, err := network.NewDecentralizedNode(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create master node: %v", err)
	}

	log.Printf("Node started successfully on port %d!", *port)

	// Wait for interrupt signal to gracefully shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
		log.Println("Received shutdown signal")
	case <-ctx.Done():
		log.Println("Context cancelled")
	}

	// Shutdown the node
	if err := masterNode.Close(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Master node stopped")
}
