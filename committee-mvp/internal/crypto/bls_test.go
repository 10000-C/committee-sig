package crypto

import (
	"errors"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
)

const testDST = "committee-sig/mvp/v1"

func TestSignShareAndVerifyShare(t *testing.T) {
	signer := mustNewSigner(t)
	pub, err := signer.PublicKey()
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	msg := []byte("hello committee")
	sig, err := signer.SignShare(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	verifier, err := NewBN254BLS(nil, testDST)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.VerifyShare(pub, msg, sig); err != nil {
		t.Fatalf("verify share: %v", err)
	}
}

func TestAggregateAndVerify(t *testing.T) {
	s1 := mustNewSigner(t)
	s2 := mustNewSigner(t)
	s3 := mustNewSigner(t)
	msg := []byte("aggregate-message")

	sig1 := mustSign(t, s1, msg)
	sig2 := mustSign(t, s2, msg)
	sig3 := mustSign(t, s3, msg)

	bitmap := []byte{0x07}
	aggSig, err := s1.AggregateSignatures([][]byte{sig1, sig2, sig3}, bitmap)
	if err != nil {
		t.Fatalf("aggregate signatures: %v", err)
	}

	pk1 := mustPublic(t, s1)
	pk2 := mustPublic(t, s2)
	pk3 := mustPublic(t, s3)
	aggPK, err := AggregatePublicKeys([][]byte{pk1, pk2, pk3}, bitmap)
	if err != nil {
		t.Fatalf("aggregate public keys: %v", err)
	}

	verifier, err := NewBN254BLS(nil, testDST)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.VerifyAggregate(aggPK, msg, aggSig, bitmap); err != nil {
		t.Fatalf("verify aggregate: %v", err)
	}
}

func TestVerifyAggregateRejectWrongMessage(t *testing.T) {
	s1 := mustNewSigner(t)
	s2 := mustNewSigner(t)
	msg := []byte("msg-1")

	sig1 := mustSign(t, s1, msg)
	sig2 := mustSign(t, s2, msg)
	bitmap := []byte{0x03}
	aggSig, err := s1.AggregateSignatures([][]byte{sig1, sig2}, bitmap)
	if err != nil {
		t.Fatalf("aggregate signatures: %v", err)
	}
	aggPK, err := AggregatePublicKeys([][]byte{mustPublic(t, s1), mustPublic(t, s2)}, bitmap)
	if err != nil {
		t.Fatalf("aggregate public keys: %v", err)
	}

	verifier, err := NewBN254BLS(nil, testDST)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	if err := verifier.VerifyAggregate(aggPK, []byte("msg-2"), aggSig, bitmap); !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("expected ErrVerifyFailed, got %v", err)
	}
}

func TestAggregateRejectDuplicateSigner(t *testing.T) {
	s1 := mustNewSigner(t)
	msg := []byte("dup")
	sig := mustSign(t, s1, msg)
	bitmap := []byte{0x03}
	if _, err := s1.AggregateSignatures([][]byte{sig, sig}, bitmap); !errors.Is(err, ErrDuplicateSigner) {
		t.Fatalf("expected ErrDuplicateSigner, got %v", err)
	}
}

func mustNewSigner(t *testing.T) *BN254BLS {
	t.Helper()
	var sk fr.Element
	if _, err := sk.SetRandom(); err != nil {
		t.Fatalf("set random key: %v", err)
	}
	s, err := NewBN254BLSFromBigInt(sk.ToBigIntRegular(new(big.Int)), testDST)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return s
}

func mustPublic(t *testing.T, s *BN254BLS) []byte {
	t.Helper()
	pk, err := s.PublicKey()
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	return pk
}

func mustSign(t *testing.T, s *BN254BLS, msg []byte) []byte {
	t.Helper()
	sig, err := s.SignShare(msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return sig
}
