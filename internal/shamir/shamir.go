// package shamir/shamir.go
package shamir

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// Split splits data into n shares where k shares are required to reconstruct
func Split(data []byte, n, k int) ([][]byte, error) {
	if k > n {
		return nil, errors.New("k cannot be greater than n")
	}
	if len(data) == 0 {
		return nil, errors.New("data cannot be empty")
	}

	// For each byte position, create a polynomial of degree k-1
	shares := make([][]byte, n)
	for i := range shares {
		shares[i] = make([]byte, len(data)+1) // +1 for x coordinate
		shares[i][0] = byte(i + 1)            // x coordinate (1-indexed)
	}

	// Process each byte of the data
	for byteIdx := 0; byteIdx < len(data); byteIdx++ {
		// Create polynomial with secret as constant term
		coefficients := make([]*big.Int, k)
		coefficients[0] = big.NewInt(int64(data[byteIdx])) // constant term is the data byte

		// Generate random coefficients for higher degree terms
		for i := 1; i < k; i++ {
			coeff, err := rand.Int(rand.Reader, big.NewInt(256))
			if err != nil {
				return nil, err
			}
			coefficients[i] = coeff
		}

		// Evaluate polynomial at n points
		for i := 0; i < n; i++ {
			x := big.NewInt(int64(i + 1))
			result := evaluatePolynomial(coefficients, x)
			shares[i][byteIdx+1] = byte(result.Uint64() % 256)
		}
	}

	return shares, nil
}

// Combine reconstructs original data from k shares
func Combine(shares [][]byte) ([]byte, error) {
	if len(shares) == 0 {
		return nil, errors.New("no shares provided")
	}

	k := len(shares)
	dataLen := len(shares[0]) - 1
	result := make([]byte, dataLen)

	// Reconstruct each byte position
	for byteIdx := 0; byteIdx < dataLen; byteIdx++ {
		points := make([]Point, k)
		for i, share := range shares {
			points[i] = Point{
				X: big.NewInt(int64(share[0])),
				Y: big.NewInt(int64(share[byteIdx+1])),
			}
		}

		// Use Lagrange interpolation to find constant term
		secret := lagrangeInterpolation(big.NewInt(0), points)
		result[byteIdx] = byte(secret.Uint64() % 256)
	}

	return result, nil
}

type Point struct {
	X, Y *big.Int
}

func evaluatePolynomial(coefficients []*big.Int, x *big.Int) *big.Int {
	result := big.NewInt(0)
	for i, coeff := range coefficients {
		term := new(big.Int).Set(coeff)
		xPower := new(big.Int).Exp(x, big.NewInt(int64(i)), nil)
		term.Mul(term, xPower)
		result.Add(result, term)
	}
	return result
}

func lagrangeInterpolation(x *big.Int, points []Point) *big.Int {
	result := big.NewInt(0)

	for i, point := range points {
		numerator := big.NewInt(1)
		denominator := big.NewInt(1)

		for j, otherPoint := range points {
			if i != j {
				// numerator *= (x - otherPoint.X)
				temp := new(big.Int).Sub(x, otherPoint.X)
				numerator.Mul(numerator, temp)

				// denominator *= (point.X - otherPoint.X)
				temp = new(big.Int).Sub(point.X, otherPoint.X)
				denominator.Mul(denominator, temp)
			}
		}

		// term = point.Y * (numerator / denominator)
		term := new(big.Int).Mul(point.Y, numerator)
		term.Div(term, denominator)
		result.Add(result, term)
	}

	return result
}
