package zksnark

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/hash"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/hash/mimc"
)

// StorageProofCircuit defines the zk-SNARK circuit for storage proofs
type StorageProofCircuit struct {
	// Public inputs (known to verifier)
	FileHash  frontend.Variable `gnark:",public"`
	Challenge frontend.Variable `gnark:",public"`

	// Private inputs (known only to prover)
	StoredData frontend.Variable
	SecretSalt frontend.Variable

	// Outputs
	Response frontend.Variable
}

// Define defines the circuit constraints
func (circuit *StorageProofCircuit) Define(api frontend.API) error {
	// Initialize MiMC hash function (zk-friendly)
	mimcHash, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// Circuit logic:
	// 1. Prove knowledge of stored data without revealing it
	// 2. Generate response = Hash(Challenge || StoredData || SecretSalt)
	// 3. Verify the response matches expected value

	// Step 1: Compute the file commitment (known to verifier)
	mimcHash.Reset()
	mimcHash.Write(circuit.StoredData, circuit.SecretSalt)
	computedFileHash := mimcHash.Sum()

	// Constraint: computed file hash must match public file hash
	api.AssertIsEqual(computedFileHash, circuit.FileHash)

	// Step 2: Compute response to challenge
	mimcHash.Reset()
	mimcHash.Write(circuit.Challenge, circuit.StoredData, circuit.SecretSalt)
	computedResponse := mimcHash.Sum()

	// Constraint: computed response must match output
	api.AssertIsEqual(computedResponse, circuit.Response)

	return nil
}

// CircuitSetup contains the proving and verification keys
type CircuitSetup struct {
	ProvingKey   groth16.ProvingKey
	VerifyingKey groth16.VerifyingKey
	R1CS         constraint.ConstraintSystem
}

// StorageProofProver handles zk-SNARK proof generation and verification
type StorageProofProver struct {
	setup *CircuitSetup
}

// NewStorageProofProver creates a new zk-SNARK prover with circuit setup
func NewStorageProofProver() (*StorageProofProver, error) {
	// Create the circuit
	var circuit StorageProofCircuit

	// Compile the circuit
	r1cs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	if err != nil {
		return nil, fmt.Errorf("failed to compile circuit: %w", err)
	}

	// Generate trusted setup (in production, this would use a trusted ceremony)
	pk, vk, err := groth16.Setup(r1cs)
	if err != nil {
		return nil, fmt.Errorf("failed to setup groth16: %w", err)
	}

	return &StorageProofProver{
		setup: &CircuitSetup{
			ProvingKey:   pk,
			VerifyingKey: vk,
			R1CS:         r1cs,
		},
	}, nil
}

// GenerateProof generates a zk-SNARK proof for storage
func (sp *StorageProofProver) GenerateProof(
	fileHash []byte,
	challenge []byte,
	storedData []byte,
	secretSalt []byte,
) ([]byte, []byte, error) {

	// Compute the response using the cryptographic MiMC
	response := sp.ComputeResponse(challenge, storedData, secretSalt)

	// Convert inputs to *big.Int for the witness
	fileHashBig := new(big.Int).SetBytes(fileHash)
	challengeBig := new(big.Int).SetBytes(challenge)
	storedDataBig := new(big.Int).SetBytes(storedData)
	secretSaltBig := new(big.Int).SetBytes(secretSalt)

	// Create witness
	assignment := StorageProofCircuit{
		FileHash:   fileHashBig,
		Challenge:  challengeBig,
		StoredData: storedDataBig,
		SecretSalt: secretSaltBig,
		Response:   response,
	}

	witness, err := frontend.NewWitness(&assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create witness: %w", err)
	}

	// Generate proof
	proof, err := groth16.Prove(sp.setup.R1CS, sp.setup.ProvingKey, witness)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate proof: %w", err)
	}

	// Serialize proof
	var proofBuf bytes.Buffer
	_, err = proof.WriteTo(&proofBuf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize proof: %w", err)
	}

	// Get public witness (for verification)
	publicWitness, err := witness.Public()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get public witness: %w", err)
	}

	var publicBuf bytes.Buffer
	_, err = publicWitness.WriteTo(&publicBuf)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize public witness: %w", err)
	}

	return proofBuf.Bytes(), publicBuf.Bytes(), nil
}

// VerifyProof verifies a zk-SNARK proof
func (sp *StorageProofProver) VerifyProof(proofBytes []byte, publicWitnessBytes []byte) error {
	// Deserialize proof
	proof := groth16.NewProof(ecc.BN254)
	proofBuf := bytes.NewReader(proofBytes)
	_, err := proof.ReadFrom(proofBuf)
	if err != nil {
		return fmt.Errorf("failed to deserialize proof: %w", err)
	}

	// Deserialize public witness
	publicWitness, err := frontend.NewWitness(nil, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}

	publicBuf := bytes.NewReader(publicWitnessBytes)
	_, err = publicWitness.ReadFrom(publicBuf)
	if err != nil {
		return fmt.Errorf("failed to deserialize public witness: %w", err)
	}

	// Verify proof
	err = groth16.Verify(proof, sp.setup.VerifyingKey, publicWitness)
	if err != nil {
		return fmt.Errorf("proof verification failed: %w", err)
	}

	return nil
}

// computeResponse computes the response for given inputs using the cryptographic MiMC
func (sp *StorageProofProver) ComputeResponse(challenge, storedData, secretSalt []byte) *big.Int {
	// Use the cryptographic MiMC implementation (not the circuit one)
	mimcHash := hash.MIMC_BN254.New()

	// Write all data to the hash
	mimcHash.Write(challenge)
	mimcHash.Write(storedData)
	mimcHash.Write(secretSalt)

	result := mimcHash.Sum(nil)

	// Convert the result to *big.Int
	return new(big.Int).SetBytes(result)
}

// GetVerifyingKey returns the verifying key for distribution
func (sp *StorageProofProver) GetVerifyingKey() ([]byte, error) {
	var buf bytes.Buffer
	_, err := sp.setup.VerifyingKey.WriteTo(&buf)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize verifying key: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadVerifyingKey loads a verifying key from bytes
func (sp *StorageProofProver) LoadVerifyingKey(vkBytes []byte) error {
	vk := groth16.NewVerifyingKey(ecc.BN254)
	buf := bytes.NewReader(vkBytes)
	_, err := vk.ReadFrom(buf)
	if err != nil {
		return fmt.Errorf("failed to deserialize verifying key: %w", err)
	}
	sp.setup.VerifyingKey = vk
	return nil
}
