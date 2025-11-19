package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ShadSpace/shadspace-go-v2/internal/network"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
)

func main() {
	// Create context that concels on interupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Determine key file path
	keyFile := "master_node_key.key"
	homeDir, err := os.UserHomeDir()
	if err == nil {
		keyFile = filepath.Join(homeDir, ".shadspace", "master_node_key.key")

		os.MkdirAll(filepath.Dir(keyFile), 0700)
	}

	// Load or generate private key
	privKey, err := utils.GetOrCreateKey(keyFile)
	if err != nil {
		log.Fatalf("Failed to load or generate private key: %v", err)
	}

	peerID, err := utils.GetPeerIDFromKey(privKey)
	if err != nil {
		log.Fatalf("Failed to get peer ID from private key: %v", err)
	}

	log.Printf("Master node Using Peer ID: %s", peerID)

	// Load configuration
	config := network.NodeConfig{
		Host:           "0.0.0.0",
		Port:           4001,
		ProtocolID:     "/shadspace/1.0.0",
		Rendezvous:     "shadspace-master-network",
		NodeType:       "master",
		BootstrapPeers: []string{},
		PrivKey:        privKey,
		KeyFile:        keyFile,
	}

	// Create and start the master node
	masterNode, err := network.NewMasterNode(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create master node %v", err)
	}

	log.Printf("Master node started successfully!")

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
	if err := masterNode.Shutdown(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Master node stopped")

}
