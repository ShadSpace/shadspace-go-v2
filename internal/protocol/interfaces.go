package protocol

import (
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/libp2p/go-libp2p/core/peer"
)

// MasterNodeHandler defines the methods that protocol handlers need from MasterNode
type MasterNodeHandler interface {
	types.MasterNodeInterface
	HandleIncomingRegistration(peerID peer.ID, req types.RegistrationRequest) error
	HandleProofOfStorage(peerID peer.ID, proof types.ProofOfStorage)
	HandleStorageOffer(peerID peer.ID, offer types.StorageOffer)
}