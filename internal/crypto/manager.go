package crypto

import (
	"crypto/rand"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/sha3"
)

// CryptoManager handles cryptographic operations for the node
type CryptoManager struct {
	privateKey crypto.PrivKey
	publicKey  crypto.PubKey
	peerID     peer.ID
}

// NewCryptoManager creates a new CryptoManager with generated keys
func NewCryptoManager() *CryptoManager {
	// Generate Ed25519 key pair using libp2p's crypto package
	privateKey, publicKey, err := crypto.GenerateKeyPair(crypto.Ed25519, -1)
	if err != nil {
		// In production, you might want to handle this differently
		panic(fmt.Sprintf("failed to generate keys: %v", err))
	}

	// Generate peer ID from public key
	peerID, err := peer.IDFromPublicKey(publicKey)
	if err != nil {
		panic(fmt.Sprintf("failed to generate peer ID: %v", err))
	}

	return &CryptoManager{
		privateKey: privateKey,
		publicKey:  publicKey,
		peerID:     peerID,
	}
}

// NewCryptoManagerFromExisting creates a CryptoManager with existing keys
func NewCryptoManagerFromExisting(privateKey crypto.PrivKey) (*CryptoManager, error) {
	// Extract public key from private key
	publicKey := privateKey.GetPublic()

	peerID, err := peer.IDFromPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate peer ID: %w", err)
	}

	return &CryptoManager{
		privateKey: privateKey,
		publicKey:  publicKey,
		peerID:     peerID,
	}, nil
}

// NewCryptoManagerFromBytes creates a CryptoManager from raw private key bytes
func NewCryptoManagerFromBytes(privateKeyBytes []byte) (*CryptoManager, error) {
	privateKey, err := crypto.UnmarshalPrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal private key: %w", err)
	}

	return NewCryptoManagerFromExisting(privateKey)
}

// Sign signs data with the node's private key
func (cm *CryptoManager) Sign(data []byte) ([]byte, error) {
	return cm.privateKey.Sign(data)
}

// Verify verifies a signature from a peer
func (cm *CryptoManager) Verify(peerID peer.ID, data []byte, signature []byte) error {
	// Extract public key from peer ID
	pubKey, err := peerID.ExtractPublicKey()
	if err != nil {
		return fmt.Errorf("failed to extract public key from peer ID: %w", err)
	}

	// Verify the signature using libp2p's crypto
	ok, err := pubKey.Verify(data, signature)
	if err != nil {
		return fmt.Errorf("signature verification error: %w", err)
	}
	if !ok {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// VerifyWithPublicKey verifies a signature with a specific public key
func (cm *CryptoManager) VerifyWithPublicKey(publicKey crypto.PubKey, data []byte, signature []byte) error {
	ok, err := publicKey.Verify(data, signature)
	if err != nil {
		return fmt.Errorf("signature verification error: %w", err)
	}
	if !ok {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// Hash computes a cryptographic hash of data
func (cm *CryptoManager) Hash(data []byte) []byte {
	hash := sha3.New256()
	hash.Write(data)
	return hash.Sum(nil)
}

// GenerateRandomBytes generates cryptographically secure random bytes
func (cm *CryptoManager) GenerateRandomBytes(length int) ([]byte, error) {
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return bytes, nil
}

// GetPeerID returns the node's peer ID
func (cm *CryptoManager) GetPeerID() peer.ID {
	return cm.peerID
}

// GetPublicKey returns the node's public key
func (cm *CryptoManager) GetPublicKey() crypto.PubKey {
	return cm.publicKey
}

// GetPrivateKey returns the node's private key (use with caution!)
func (cm *CryptoManager) GetPrivateKey() crypto.PrivKey {
	return cm.privateKey
}

// GetPrivateKeyBytes returns the marshaled private key bytes
func (cm *CryptoManager) GetPrivateKeyBytes() ([]byte, error) {
	return crypto.MarshalPrivateKey(cm.privateKey)
}

// GetPublicKeyBytes returns the marshaled public key bytes
func (cm *CryptoManager) GetPublicKeyBytes() ([]byte, error) {
	return crypto.MarshalPublicKey(cm.publicKey)
}

// PeerIDFromPublicKey creates a peer ID from any public key
func (cm *CryptoManager) PeerIDFromPublicKey(publicKey crypto.PubKey) (peer.ID, error) {
	return peer.IDFromPublicKey(publicKey)
}

// GenerateKeyPair generates a new Ed25519 key pair (useful for testing or key rotation)
func (cm *CryptoManager) GenerateKeyPair() (crypto.PrivKey, crypto.PubKey, error) {
	return crypto.GenerateKeyPair(crypto.Ed25519, -1)
}

// GenerateRSAKeyPair generates a new RSA key pair
func (cm *CryptoManager) GenerateRSAKeyPair(bits int) (crypto.PrivKey, crypto.PubKey, error) {
	return crypto.GenerateKeyPair(crypto.RSA, bits)
}
