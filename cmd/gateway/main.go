// cmd/gateway/main.go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ShadSpace/shadspace-go-v2/internal/gateway"
	"github.com/ShadSpace/shadspace-go-v2/internal/network"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
)

func main() {
	// Parse command-line flags
	keyFile := flag.String("key", "", "Path to private key file")
	apiPort := flag.Int("api-port", 8080, "Port for HTTP API")
	nodePort := flag.Int("node-port", 4015, "Port for P2P node")
	bootstrapPeer := flag.String("bootstrap", "", "Bootstrap peer address (optional)")
	flag.Parse()

	// Validate inputs
	if *keyFile == "" {
		defaultKeyFile := filepath.Join("keys", "gateway_node.key")
		if err := os.MkdirAll(filepath.Dir(defaultKeyFile), 0700); err != nil {
			log.Fatalf("Failed to create keys directory: %v", err)
		}
		*keyFile = defaultKeyFile
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Load or generate private key
	privKey, err := utils.GetOrCreateKey(*keyFile)
	if err != nil {
		log.Fatalf("Failed to load or generate private key: %v", err)
	}

	// Set up bootstrap peers
	bootstrapPeers := []string{
		// Default bootstrap peers - adjust these based on your network
		"/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWFgQMSugNGS6mW4drX2N5oQsAF7Jxa9CWgy8bAQaSHJaU",
	}

	// Add custom bootstrap peer if provided
	if *bootstrapPeer != "" {
		bootstrapPeers = append(bootstrapPeers, *bootstrapPeer)
	}

	// Create node configuration
	config := network.NodeConfig{
		Host:           "0.0.0.0",
		Port:           *nodePort,
		ProtocolID:     "/shadspace/1.0.0",
		Rendezvous:     "shadspace-gateway-network",
		NodeType:       "gateway",
		BootstrapPeers: bootstrapPeers,
		PrivKey:        privKey,
		KeyFile:        *keyFile,
	}

	log.Printf("🚀 Starting ShadSpace Gateway...")
	log.Printf("   📍 API Port: %d", *apiPort)
	log.Printf("   🔌 P2P Port: %d", *nodePort)
	log.Printf("   🔑 Key File: %s", *keyFile)
	log.Printf("   🌐 Bootstrap Peers: %v", bootstrapPeers)

	// Create gateway node
	gatewayNode, err := gateway.NewGatewayNode(ctx, config, *apiPort)
	if err != nil {
		log.Fatalf("Failed to create gateway node: %v", err)
	}
	defer gatewayNode.Close()

	// Start the gateway node
	if err := gatewayNode.Start(); err != nil {
		log.Fatalf("Failed to start gateway node: %v", err)
	}

	log.Printf("✅ ShadSpace Gateway is running!")
	log.Printf("   🌐 API: http://localhost:%d", *apiPort)
	log.Printf("   📡 P2P: port %d", *nodePort)
	log.Printf("   🆔 Node ID: %s", gatewayNode.GetGatewayInfo().PeerID)
	log.Printf("   ⏰ Uptime: %s", gatewayNode.GetGatewayInfo().Uptime)

	// Wait for shutdown signal
	<-ctx.Done()

	log.Println("👋 Shutting down gateway node...")

	// The defer will handle the actual Close() call
	log.Println("✅ Gateway node shutdown complete")
}
