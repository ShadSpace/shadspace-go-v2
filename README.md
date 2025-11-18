# Decentralized Storage System

A distributed, scalable storage network built in Go that allows users to join as farmers to provide storage capacity and earn rewards through token economics.

## 🚀 Features

- **Distributed Architecture**: Master nodes coordinate farmer nodes in a peer-to-peer network
- **Zero-Knowledge Proofs**: Cryptographic verification of storage without revealing data
- **Token Economics**: Reward system for farmers providing storage services
- **Bitswap Protocol**: Efficient data exchange between nodes
- **SHA-256 Encryption**: Secure file encryption and hashing
- **Shamir Secret Sharing**: Distributed key management for enhanced security
- **Scalable Design**: Horizontal scaling support for large networks
- **Metrics Tracking**: Comprehensive monitoring of network performance and economics

## 🏗️ Architecture

### Components

- **Master Nodes**: Coordinate the network, track farmers, and manage file locations
- **Farmer Nodes**: Provide storage capacity and serve client requests
- **Clients**: Upload and retrieve files from the network

### Key Modules

- **Network Layer**: Handles node communication and bitswap protocol
- **Storage Engine**: Manages file chunking, encryption, and storage
- **Cryptography**: Implements SHA-256, Shamir Secret Sharing, and ZK proofs
- **Consensus**: Proof-of-Stake mechanism for network agreement
- **Metrics**: Tracks storage, network, and economic metrics

## 📁 Project Structure

## 🛠️ Installation

### Prerequisites

- Go 1.19 or higher
- Protocol Buffer compiler

### Build from Source

```bash
shadspace-go-v2/
├── cmd/
│ ├── master-node/
│ │ └── main.go # Master node entry point with libp2p
│ ├── farmer-node/
│ │ └── main.go # Farmer node entry point with libp2p
│ └── client/
│ └── main.go # Client entry point with libp2p
├── internal/
│ ├── libp2p/ # NEW: Libp2p network layer
│ │ ├── node.go # Base libp2p node implementation
│ │ ├── master_node.go # Master node services with libp2p
│ │ ├── farmer_node.go # Farmer node services with libp2p
│ │ ├── client_node.go # Client services with libp2p
│ │ ├── bitswap.go # Bitswap protocol implementation
│ │ └── pubsub.go # PubSub messaging handlers
│ ├── storage/
│ │ ├── engine.go # Storage engine and chunk management
│ │ ├── chunk_manager.go # File chunking and distribution
│ │ └── replication.go # Data replication logic
│ ├── crypto/
│ │ ├── manager.go # Crypto operations (SHA-256, Shamir, ZK)
│ │ ├── zk_proofs.go # Zero-knowledge proof implementations
│ │ └── shamir.go # Shamir secret sharing
│ ├── consensus/
│ │ ├── pos.go # Proof-of-Stake consensus
│ │ ├── validator.go # Validator management
│ │ └── block.go # Block structure and validation
│ ├── metrics/
│ │ ├── collector.go # Metrics collection
│ │ ├── tracker.go # Performance tracking
│ │ └── economics.go # Token economics tracking
│ └── protocol/ # NEW: Protocol definitions
│ ├── messages.proto # Protocol buffer definitions
│ ├── bitswap.proto # Bitswap protocol messages
│ └── storage.proto # Storage protocol messages
├── pkg/
│ ├── types/
│ │ └── types.go # Common types and interfaces
│ ├── utils/
│ │ ├── helpers.go # Utility functions
│ │ ├── file_utils.go # File handling utilities
│ │ └── network_utils.go # Network utilities
│ └── config/
│ ├── config.go # Configuration management
│ ├── master.yaml # Master node configuration
│ ├── farmer.yaml # Farmer node configuration
│ └── client.yaml # Client configuration
├── scripts/
│ ├── build.sh # Build scripts
│ ├── deploy.sh # Deployment scripts
│ └── test.sh # Test scripts
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

```bash
# Clone the repository
git clone https://github.com/your-org/decentralized-storage.git
cd decentralized-storage

# Build all components
go build -o bin/master-node ./cmd/master-node
go build -o bin/farmer-node ./cmd/farmer-node
go build -o bin/client ./cmd/client

# Generate protocol buffers
protoc --go_out=. pkg/protobuf/*.proto
```
# shadspace-go-v2
