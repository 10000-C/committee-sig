package share

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
)

var frModulus, _ = new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)

var (
	ErrDuplicateShareIndex = errors.New("duplicate share index")
	ErrInvalidThreshold    = errors.New("invalid threshold")
	ErrInsufficientShares  = errors.New("insufficient shares")
)

type Share struct {
	Index uint64
	Value *big.Int
}

func Split(secret *big.Int, t, n int) ([]Share, error) {
	return SplitWithRand(secret, t, n, rand.Reader)
}

func SplitWithRand(secret *big.Int, t, n int, rnd io.Reader) ([]Share, error) {
	if secret == nil {
		return nil, errors.New("secret is nil")
	}
	if t < 2 || n < t {
		return nil, ErrInvalidThreshold
	}

	coeffs := make([]*big.Int, t)
	coeffs[0] = mod(new(big.Int).Set(secret))
	for i := 1; i < t; i++ {
		v, err := randInt(rnd, frModulus)
		if err != nil {
			return nil, fmt.Errorf("sample coefficient: %w", err)
		}
		coeffs[i] = v
	}

	shares := make([]Share, 0, n)
	for i := 1; i <= n; i++ {
		x := big.NewInt(int64(i))
		y := evalPoly(coeffs, x)
		shares = append(shares, Share{Index: uint64(i), Value: y})
	}
	return shares, nil
}

func Recover(shares []Share, t int) (*big.Int, error) {
	if t < 2 {
		return nil, ErrInvalidThreshold
	}
	if len(shares) < t {
		return nil, ErrInsufficientShares
	}
	seen := make(map[uint64]struct{}, len(shares))
	for _, s := range shares {
		if s.Value == nil {
			return nil, errors.New("share value is nil")
		}
		if _, ok := seen[s.Index]; ok {
			return nil, ErrDuplicateShareIndex
		}
		seen[s.Index] = struct{}{}
	}

	acc := big.NewInt(0)
	for i := 0; i < t; i++ {
		xi := big.NewInt(int64(shares[i].Index))
		yi := mod(new(big.Int).Set(shares[i].Value))

		numerator := big.NewInt(1)
		denominator := big.NewInt(1)
		for j := 0; j < t; j++ {
			if i == j {
				continue
			}
			xj := big.NewInt(int64(shares[j].Index))
			numerator = mod(new(big.Int).Mul(numerator, new(big.Int).Neg(xj)))
			diff := mod(new(big.Int).Sub(xi, xj))
			denominator = mod(new(big.Int).Mul(denominator, diff))
		}
		invDenominator := new(big.Int).ModInverse(denominator, frModulus)
		if invDenominator == nil {
			return nil, errors.New("failed to invert denominator")
		}
		li0 := mod(new(big.Int).Mul(numerator, invDenominator))
		term := mod(new(big.Int).Mul(yi, li0))
		acc = mod(new(big.Int).Add(acc, term))
	}
	return acc, nil
}

func evalPoly(coeffs []*big.Int, x *big.Int) *big.Int {
	acc := big.NewInt(0)
	xPow := big.NewInt(1)
	for _, c := range coeffs {
		term := mod(new(big.Int).Mul(c, xPow))
		acc = mod(new(big.Int).Add(acc, term))
		xPow = mod(new(big.Int).Mul(xPow, x))
	}
	return acc
}

func randInt(r io.Reader, max *big.Int) (*big.Int, error) {
	v, err := rand.Int(r, max)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func mod(v *big.Int) *big.Int {
	v.Mod(v, frModulus)
	if v.Sign() < 0 {
		v.Add(v, frModulus)
	}
	return v
}
