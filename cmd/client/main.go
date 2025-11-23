package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/network"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
)

func main() {
	// Parse command-line flags
	keyFile := flag.String("key", "", "Path to private key file")
	port := flag.Int("port", 4001, "Port to listen on")
	filePath := flag.String("file", "", "Path to file to upload")
	action := flag.String("action", "upload", "Action to perform: upload, retrieve, list, stats")
	fileHash := flag.String("hash", "", "File hash for retrieval (required for retrieve action)")
	flag.Parse()

	// Validate inputs
	if *keyFile == "" {
		defaultKeyFile := filepath.Join("keys", "test_node.key")
		if err := os.MkdirAll(filepath.Dir(defaultKeyFile), 0700); err != nil {
			log.Fatalf("Failed to create keys directory: %v", err)
		}
		*keyFile = defaultKeyFile
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load or generate private key
	privKey, err := utils.GetOrCreateKey(*keyFile)
	if err != nil {
		log.Fatalf("Failed to load or generate private key: %v", err)
	}

	// Create node configuration
	config := network.NodeConfig{
		Host:       "0.0.0.0",
		Port:       *port,
		ProtocolID: "/shadspace/1.0.0",
		Rendezvous: "shadspace-test-network",
		NodeType:   "test",
		BootstrapPeers: []string{
			"/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWFgQMSugNGS6mW4drX2N5oQsAF7Jxa9CWgy8bAQaSHJaU",
		},
		PrivKey: privKey,
		KeyFile: *keyFile,
	}

	// Create decentralized node
	node, err := network.NewDecentralizedNode(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	log.Printf("Test node started successfully on port %d", *port)
	log.Printf("Node ID: %s", node.GetHostID())

	// Wait a bit for node to initialize
	time.Sleep(2 * time.Second)

	// Perform the requested action
	switch *action {
	case "upload":
		if *filePath == "" {
			log.Fatal("File path is required for upload action")
		}
		testFileUpload(node, *filePath)
	case "retrieve":
		if *fileHash == "" {
			log.Fatal("File hash is required for retrieve action")
		}
	case "list":
		testListFiles(node)

	default:
		log.Fatalf("Unknown action: %s", *action)
	}

	log.Println("Test completed successfully")
}

// testFileUpload tests file upload functionality
func testFileUpload(node *network.DecentralizedNode, filePath string) {
	log.Printf("Attempting to upload file: %s", filePath)

	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	fileName := filepath.Base(filePath)
	log.Printf("File details: %s, size: %d bytes", fileName, len(data))

	// Select some dummy peers for storage (in real scenario, these would be actual peers)
	dummyPeers := []string{"peer1", "peer2", "peer3"}

	// Convert to peer.ID (in real implementation, these would be actual peer IDs)
	storagePeers := make([]string, len(dummyPeers))
	copy(storagePeers, dummyPeers)

	// Store the file
	startTime := time.Now()
	fileHash, err := node.GetStorageOperations().StoreFileDistributed(fileName, data, 3, 2)
	if err != nil {
		log.Fatalf("Failed to store file: %v", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ File uploaded successfully!")
	log.Printf("   File Hash: %s", fileHash)
	log.Printf("   File Name: %s", fileName)
	log.Printf("   File Size: %d bytes", len(data))
	log.Printf("   Upload Time: %v", duration)
	log.Printf("   Shards: 3 total, 2 required for reconstruction")

	// Verify file integrity
	log.Printf("Verifying file integrity...")
	integrity, err := node.GetFileManager().VerifyFileIntegrity(fileHash)
	if err != nil {
		log.Printf("Warning: Failed to verify file integrity: %v", err)
	} else if integrity {
		log.Printf("✅ File integrity verified successfully")
	} else {
		log.Printf("❌ File integrity check failed")
	}

	// Get file info
	fileInfo, err := node.GetFileManager().GetFileInfo(fileHash)
	if err != nil {
		log.Printf("Warning: Failed to get file info: %v", err)
	} else {
		log.Printf("File Metadata:")
		log.Printf("   - Created: %v", fileInfo.CreatedAt.Format(time.RFC3339))
		log.Printf("   - Total Shards: %d", fileInfo.TotalShards)
		log.Printf("   - Required Shards: %d", fileInfo.RequiredShards)
		log.Printf("   - Stored Peers: %d", len(fileInfo.StoredPeers))
	}
}

// testFileRetrieve tests file retrieval functionality
func testFileRetrieve(node *network.DecentralizedNode, fileHash string) {
	log.Printf("Attempting to retrieve file with hash: %s", fileHash)

	startTime := time.Now()
	data, err := node.GetStorageOperations().RetrieveFileDistributed(fileHash)
	if err != nil {
		log.Fatalf("Failed to retrieve file: %v", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ File retrieved successfully!")
	log.Printf("   File Hash: %s", fileHash)
	log.Printf("   File Size: %d bytes", len(data))
	log.Printf("   Retrieval Time: %v", duration)

	// Save retrieved file to disk for verification
	outputPath := fmt.Sprintf("retrieved_%s.bin", fileHash[:8])
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Printf("Warning: Failed to save retrieved file: %v", err)
	} else {
		log.Printf("   Saved to: %s", outputPath)
	}

	// Verify the retrieved data matches the expected hash
	calculatedHash := node.GetFileManager().CalculateFileHash(data)
	if calculatedHash == fileHash {
		log.Printf("✅ Retrieved file hash matches expected hash")
	} else {
		log.Printf("❌ Hash mismatch! Expected: %s, Got: %s", fileHash, calculatedHash)
	}
}

// testListFiles lists all stored files
func testListFiles(node *network.DecentralizedNode) {
	log.Printf("Listing all stored files...")

	files := node.GetFileManager().ListFiles()
	if len(files) == 0 {
		log.Printf("No files stored")
		return
	}

	log.Printf("Found %d stored files:", len(files))
	for i, file := range files {
		log.Printf("%d. %s", i+1, file.FileName)
		log.Printf("   Hash: %s", file.FileHash)
		log.Printf("   Size: %d bytes", file.FileSize)
		log.Printf("   Shards: %d/%d", file.TotalShards, file.RequiredShards)
		log.Printf("   Created: %v", file.CreatedAt.Format(time.RFC3339))
		log.Printf("   Stored on: %d peers", len(file.StoredPeers))
		log.Printf("")
	}
}

// testStorageStats shows storage statistics
func testStorageStats(node *network.DecentralizedNode) {
	log.Printf("Storage Statistics:")

	// File manager stats
	fileStats := node.GetFileManager().GetFileStats()
	log.Printf("File Statistics:")
	log.Printf("   Total Files: %v", fileStats["total_files"])
	log.Printf("   Total Size: %v bytes", fileStats["total_file_size"])
	log.Printf("   Average File Size: %.2f bytes", fileStats["average_file_size"])

	// Storage engine stats
	storageStats := node.GetStorageEngine().GetStats()
	log.Printf("Storage Engine Statistics:")
	log.Printf("   Total Chunks: %v", storageStats["total_chunks"])
	log.Printf("   Used Bytes: %v", storageStats["used_bytes"])
	log.Printf("   Available Bytes: %v", storageStats["available_bytes"])
	log.Printf("   Usage Percentage: %.2f%%", storageStats["usage_percentage"])

	// Storage operations stats
	opsStats := node.GetStorageOperations().GetStorageStats()
	log.Printf("Storage Operations:")
	log.Printf("   Pending Operations: %v", opsStats["pending_operations"])
	log.Printf("   Completed Operations: %v", opsStats["completed_operations"])
	log.Printf("   Failed Operations: %v", opsStats["failed_operations"])
}
