package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ShadSpace/shadspace-go-v2/internal/network"
)

func main() {
	// Create context that cancels on interrupt signal
	ctx, cancel := context.WithCancel(context.Background()) // Fixed: added () and :=
	defer cancel()

	// Load configuration
	config := network.NodeConfig{ // Fixed: use {} instead of ()
		Host:       "0.0.0.0",
		Port:       0, // Random port
		ProtocolID: "/shadspace/1.0.0",
		Rendezvous: "shadspace-master-network",
		NodeType:   "farmer",
		BootstrapPeers: []string{
			"/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWJ8vBDHHfPsj63Qgt1KvQzGDEko9PgwgXzxm4eRRtEW3K",
		},
	}

	// Create and start the farmer node - Fixed: added error handling
	farmerNode, err := network.NewFarmerNode(ctx, config) // Fixed: corrected package name
	if err != nil {
		log.Fatalf("Failed to create farmer node: %v", err)
	}

	log.Printf("Farmer node started successfully!")
	log.Printf("Farmer ID: %s", farmerNode.GetHost().ID())

	// Wait for interrupt signal to gracefully shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select { // Fixed: added space before {
	case <-sigCh: // Fixed: added space
		log.Println("Received shutdown signal") // Fixed: spelling
	case <-ctx.Done():
		log.Println("Context cancelled")
	}

	// Shutdown the node
	if err := farmerNode.Shutdown(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Farmer node stopped")
}
