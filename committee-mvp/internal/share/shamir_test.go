package share

import (
	"crypto/rand"
	"errors"
	"math/big"
	"testing"
)

func TestSplitRecoverThreshold(t *testing.T) {
	secret, err := rand.Int(rand.Reader, frModulus)
	if err != nil {
		t.Fatalf("rand secret: %v", err)
	}
	shares, err := Split(secret, 5, 8)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	recovered, err := Recover(shares[:5], 5)
	if err != nil {
		t.Fatalf("recover failed: %v", err)
	}
	if recovered.Cmp(secret) != 0 {
		t.Fatalf("secret mismatch got=%s want=%s", recovered.String(), secret.String())
	}
}

func TestRecoverInsufficientShares(t *testing.T) {
	secret := big.NewInt(12345)
	shares, err := Split(secret, 5, 8)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	if _, err := Recover(shares[:4], 5); !errors.Is(err, ErrInsufficientShares) {
		t.Fatalf("expected insufficient shares error, got: %v", err)
	}
}

func TestRecoverDuplicateIndex(t *testing.T) {
	secret := big.NewInt(777)
	shares, err := Split(secret, 3, 5)
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}
	dup := []Share{shares[0], shares[1], shares[1]}
	if _, err := Recover(dup, 3); !errors.Is(err, ErrDuplicateShareIndex) {
		t.Fatalf("expected duplicate index error, got: %v", err)
	}
}

func TestPropertySplitRecover1000(t *testing.T) {
	const rounds = 1000
	for i := 0; i < rounds; i++ {
		secret, err := rand.Int(rand.Reader, frModulus)
		if err != nil {
			t.Fatalf("round %d: rand secret: %v", i, err)
		}
		shares, err := Split(secret, 5, 8)
		if err != nil {
			t.Fatalf("round %d: split: %v", i, err)
		}
		recovered, err := Recover([]Share{shares[1], shares[3], shares[4], shares[6], shares[7]}, 5)
		if err != nil {
			t.Fatalf("round %d: recover: %v", i, err)
		}
		if recovered.Cmp(secret) != 0 {
			t.Fatalf("round %d: mismatch", i)
		}
	}
}
