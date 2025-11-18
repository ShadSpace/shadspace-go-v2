package main

import (
	"os"
	"log"
	"context"
	"os/signal"
	"syscall"
	
	"github.com/ShadSpace/shadspace-go-v2/internal/network"
)

func main() {
	// Create context that concels on interupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load configuration
	config := network.NodeConfig{
		Host:          "0.0.0.0",
		Port: 		   4001,
		ProtocolID:    "/shadspace/1.0.0",
		Rendezvous:    "shadspace-master-network",
		NodeType:	   "master",
		BootstrapPeers: []string{},
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