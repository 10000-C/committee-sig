package crypto

import (
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"sort"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

var (
	ErrNotImplemented    = errors.New("not implemented")
	ErrMissingPrivateKey = errors.New("missing private key")
	ErrInvalidBitmap     = errors.New("invalid bitmap")
	ErrDuplicateSigner   = errors.New("duplicate signer")
	ErrInvalidPoint      = errors.New("invalid curve point encoding")
	ErrVerifyFailed      = errors.New("signature verification failed")
)

type Signer interface {
	SignShare(message []byte) ([]byte, error)
	VerifyShare(pubkey, message, sig []byte) error
	AggregateSignatures(sigs [][]byte, bitmap []byte) ([]byte, error)
	VerifyAggregate(aggregatePub, message, aggregateSig, bitmap []byte) error
}

// BN254BLS provides a minimal BLS-style scheme on BN254:
// - public keys live in G2
// - signatures live in G1
// - verify with pairing equation e(sig, g2) == e(H(m), pk)
type BN254BLS struct {
	privateKey *big.Int
	dst        []byte
}

func NewBN254BLS(privateKey []byte, dst string) (*BN254BLS, error) {
	if dst == "" {
		return nil, errors.New("empty domain separation tag")
	}
	b := &BN254BLS{dst: []byte(dst)}
	if len(privateKey) == 0 {
		return b, nil
	}
	var sk fr.Element
	if err := sk.SetBytesCanonical(privateKey); err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	b.privateKey = sk.ToBigIntRegular(new(big.Int))
	return b, nil
}

func NewBN254BLSFromBigInt(privateKey *big.Int, dst string) (*BN254BLS, error) {
	if dst == "" {
		return nil, errors.New("empty domain separation tag")
	}
	b := &BN254BLS{dst: []byte(dst)}
	if privateKey == nil {
		return b, nil
	}
	var sk fr.Element
	sk.SetBigInt(privateKey)
	b.privateKey = sk.ToBigIntRegular(new(big.Int))
	return b, nil
}

func (b *BN254BLS) PublicKey() ([]byte, error) {
	if b.privateKey == nil {
		return nil, ErrMissingPrivateKey
	}
	_, _, _, g2 := bn254.Generators()
	pk := new(bn254.G2Affine).ScalarMultiplication(&g2, b.privateKey)
	return pk.Marshal(), nil
}

func (b *BN254BLS) SignShare(message []byte) ([]byte, error) {
	if b.privateKey == nil {
		return nil, ErrMissingPrivateKey
	}
	h, err := bn254.HashToG1(message, b.dst)
	if err != nil {
		return nil, fmt.Errorf("hash message: %w", err)
	}
	sig := new(bn254.G1Affine).ScalarMultiplication(&h, b.privateKey)
	return sig.Marshal(), nil
}

func (b *BN254BLS) VerifyShare(pubkey, message, sig []byte) error {
	if len(pubkey) == 0 || len(sig) == 0 {
		return ErrInvalidPoint
	}
	var pk bn254.G2Affine
	if err := pk.Unmarshal(pubkey); err != nil {
		return fmt.Errorf("decode public key: %w", ErrInvalidPoint)
	}
	var s bn254.G1Affine
	if err := s.Unmarshal(sig); err != nil {
		return fmt.Errorf("decode signature: %w", ErrInvalidPoint)
	}
	h, err := bn254.HashToG1(message, b.dst)
	if err != nil {
		return fmt.Errorf("hash message: %w", err)
	}
	negH := new(bn254.G1Affine).Neg(&h)
	_, _, _, g2 := bn254.Generators()
	ok, err := bn254.PairingCheck([]bn254.G1Affine{s, *negH}, []bn254.G2Affine{g2, pk})
	if err != nil {
		return fmt.Errorf("pairing check failed: %w", err)
	}
	if !ok {
		return ErrVerifyFailed
	}
	return nil
}

func (b *BN254BLS) AggregateSignatures(sigs [][]byte, bitmap []byte) ([]byte, error) {
	expected, err := validateBitmap(bitmap)
	if err != nil {
		return nil, err
	}
	if expected != len(sigs) {
		return nil, fmt.Errorf("bitmap/signature count mismatch: bits=%d sigs=%d", expected, len(sigs))
	}
	seen := make(map[string]struct{}, len(sigs))
	var acc bn254.G1Affine
	set := false
	for _, encSig := range sigs {
		if len(encSig) == 0 {
			return nil, ErrInvalidPoint
		}
		k := string(encSig)
		if _, ok := seen[k]; ok {
			return nil, ErrDuplicateSigner
		}
		seen[k] = struct{}{}

		var p bn254.G1Affine
		if err := p.Unmarshal(encSig); err != nil {
			return nil, fmt.Errorf("decode signature: %w", ErrInvalidPoint)
		}
		if !set {
			acc.Set(&p)
			set = true
			continue
		}
		acc.Add(&acc, &p)
	}
	if !set {
		return nil, ErrInvalidBitmap
	}
	return acc.Marshal(), nil
}

func (b *BN254BLS) VerifyAggregate(aggregatePub, message, aggregateSig, bitmap []byte) error {
	if _, err := validateBitmap(bitmap); err != nil {
		return err
	}
	if len(aggregatePub) == 0 || len(aggregateSig) == 0 {
		return ErrInvalidPoint
	}
	var aggPK bn254.G2Affine
	if err := aggPK.Unmarshal(aggregatePub); err != nil {
		return fmt.Errorf("decode aggregate public key: %w", ErrInvalidPoint)
	}
	var aggSig bn254.G1Affine
	if err := aggSig.Unmarshal(aggregateSig); err != nil {
		return fmt.Errorf("decode aggregate signature: %w", ErrInvalidPoint)
	}

	h, err := bn254.HashToG1(message, b.dst)
	if err != nil {
		return fmt.Errorf("hash message: %w", err)
	}
	negH := new(bn254.G1Affine).Neg(&h)
	_, _, _, g2 := bn254.Generators()
	ok, err := bn254.PairingCheck(
		[]bn254.G1Affine{aggSig, *negH},
		[]bn254.G2Affine{g2, aggPK},
	)
	if err != nil {
		return fmt.Errorf("pairing check failed: %w", err)
	}
	if !ok {
		return ErrVerifyFailed
	}
	return nil
}

// AggregateThresholdSignatures combines partial signatures via Lagrange interpolation at x=0.
// signerIndices are zero-based committee indices aligned with sigs.
func AggregateThresholdSignatures(sigs [][]byte, signerIndices []int) ([]byte, error) {
	if len(sigs) == 0 || len(sigs) != len(signerIndices) {
		return nil, ErrInvalidBitmap
	}
	type pair struct {
		idx int
		sig []byte
	}
	pairs := make([]pair, 0, len(sigs))
	seen := make(map[int]struct{}, len(sigs))
	for i := range sigs {
		if len(sigs[i]) == 0 {
			return nil, ErrInvalidPoint
		}
		if signerIndices[i] < 0 {
			return nil, ErrInvalidBitmap
		}
		if _, ok := seen[signerIndices[i]]; ok {
			return nil, ErrDuplicateSigner
		}
		seen[signerIndices[i]] = struct{}{}
		pairs = append(pairs, pair{idx: signerIndices[i], sig: sigs[i]})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })

	xs := make([]*big.Int, 0, len(pairs))
	points := make([]bn254.G1Affine, 0, len(pairs))
	for _, p := range pairs {
		x := big.NewInt(int64(p.idx + 1))
		xs = append(xs, x)
		var sig bn254.G1Affine
		if err := sig.Unmarshal(p.sig); err != nil {
			return nil, fmt.Errorf("decode signature: %w", ErrInvalidPoint)
		}
		points = append(points, sig)
	}

	var acc bn254.G1Affine
	set := false
	for i := range points {
		lambda, err := lagrangeCoeffAtZero(xs, i)
		if err != nil {
			return nil, err
		}
		var li fr.Element
		li.SetBigInt(lambda)
		coeff := li.ToBigIntRegular(new(big.Int))
		term := new(bn254.G1Affine).ScalarMultiplication(&points[i], coeff)
		if !set {
			acc.Set(term)
			set = true
			continue
		}
		acc.Add(&acc, term)
	}
	if !set {
		return nil, ErrInvalidBitmap
	}
	return acc.Marshal(), nil
}

func lagrangeCoeffAtZero(xs []*big.Int, i int) (*big.Int, error) {
	if i < 0 || i >= len(xs) {
		return nil, ErrInvalidBitmap
	}
	mod := fr.Modulus()
	num := big.NewInt(1)
	den := big.NewInt(1)
	xi := new(big.Int).Set(xs[i])
	for j := range xs {
		if j == i {
			continue
		}
		xj := new(big.Int).Set(xs[j])
		negXj := new(big.Int).Neg(xj)
		negXj.Mod(negXj, mod)
		num.Mul(num, negXj)
		num.Mod(num, mod)

		diff := new(big.Int).Sub(xi, xj)
		diff.Mod(diff, mod)
		den.Mul(den, diff)
		den.Mod(den, mod)
	}
	inv := new(big.Int).ModInverse(den, mod)
	if inv == nil {
		return nil, ErrInvalidBitmap
	}
	out := new(big.Int).Mul(num, inv)
	out.Mod(out, mod)
	return out, nil
}

func AggregatePublicKeys(pubkeys [][]byte, bitmap []byte) ([]byte, error) {
	expected, err := validateBitmap(bitmap)
	if err != nil {
		return nil, err
	}
	if expected != len(pubkeys) {
		return nil, fmt.Errorf("bitmap/public-key count mismatch: bits=%d pubkeys=%d", expected, len(pubkeys))
	}
	seen := make(map[string]struct{}, len(pubkeys))
	var acc bn254.G2Affine
	set := false
	for _, encPK := range pubkeys {
		if len(encPK) == 0 {
			return nil, ErrInvalidPoint
		}
		k := string(encPK)
		if _, ok := seen[k]; ok {
			return nil, ErrDuplicateSigner
		}
		seen[k] = struct{}{}

		var p bn254.G2Affine
		if err := p.Unmarshal(encPK); err != nil {
			return nil, fmt.Errorf("decode public key: %w", ErrInvalidPoint)
		}
		if !set {
			acc.Set(&p)
			set = true
			continue
		}
		acc.Add(&acc, &p)
	}
	if !set {
		return nil, ErrInvalidBitmap
	}
	return acc.Marshal(), nil
}

func validateBitmap(bitmap []byte) (int, error) {
	if len(bitmap) == 0 {
		return 0, ErrInvalidBitmap
	}
	count := 0
	for _, b := range bitmap {
		count += bits.OnesCount8(uint8(b))
	}
	if count == 0 {
		return 0, ErrInvalidBitmap
	}
	return count, nil
}
