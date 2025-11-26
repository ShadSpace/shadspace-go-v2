package utils

import (
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// GenerateAndSaveKey generates a new private key and saves it to file
func GenerateAndSaveKey(keyFile string) (crypto.PrivKey, error) {
	// Generate a new private key
	privKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Marshal the private key to bytes
	keyBytes, err := crypto.MarshalPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// save to file
	err = ioutil.WriteFile(keyFile, keyBytes, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to write private key to file: %w", err)
	}

	log.Printf("Generated new private key and saved to %s", keyFile)
	return privKey, nil
}

// LoadKey loads a private key from file
func LoadKey(keyFile string) (crypto.PrivKey, error) {
	keyBytes, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key from file: %w", err)
	}

	privKey, err := crypto.UnmarshalPrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal private key: %w", err)
	}

	log.Printf("Loaded private key from %s", keyFile)
	return privKey, nil
}

// GetOrCreateKey either loads an existing key or generates a new one
func GetOrCreateKey(keyFile string) (crypto.PrivKey, error) {
	// check if key file exists
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return GenerateAndSaveKey(keyFile)
	}

	return LoadKey(keyFile)
}

// GetPeerIDFromKey returns the peer ID from a private key (for logging/ debugging)
func GetPeerIDFromKey(privKey crypto.PrivKey) (string, error) {
	pubKey := privKey.GetPublic()
	peerID, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("failed to get peer ID from public key: %w", err)
	}

	return peerID.String(), nil
}

func GenerateNodeName(peerID peer.ID) string {
	// Use last 8 characters of peer ID for name
	idStr := peerID.String()
	if len(idStr) > 8 {
		return "Node-" + idStr[len(idStr)-8:]
	}
	return "Node-" + idStr
}

func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Helper function to round to 2 decimal places
func RoundToTwoDecimals(num float64) float64 {
	return float64(int(num*100)) / 100
}

func GetNodeAddresses(host host.Host) []string {
	var addresses []string
	for _, addr := range host.Addrs() {
		addresses = append(addresses, fmt.Sprintf("%s/p2p/%s", addr, host.ID()))
	}
	return addresses
}

func GenerateTemporaryKey() (crypto.PrivKey, error) {
	privKey, _, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		return nil, fmt.Errorf("failed to generate temporary key: %w", err)
	}
	return privKey, nil
}
