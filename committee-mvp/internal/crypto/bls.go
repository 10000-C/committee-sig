package crypto

import "errors"

var ErrNotImplemented = errors.New("not implemented")

type Signer interface {
	SignShare(message []byte) ([]byte, error)
	VerifyShare(pubkey, message, sig []byte) error
	AggregateSignatures(sigs [][]byte, bitmap []byte) ([]byte, error)
	VerifyAggregate(aggregatePub, message, aggregateSig, bitmap []byte) error
}

// BN254BLS will be implemented using gnark-crypto in the next phase.
type BN254BLS struct{}

func (b *BN254BLS) SignShare(_ []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}

func (b *BN254BLS) VerifyShare(_, _, _ []byte) error {
	return ErrNotImplemented
}

func (b *BN254BLS) AggregateSignatures(_ [][]byte, _ []byte) ([]byte, error) {
	return nil, ErrNotImplemented
}

func (b *BN254BLS) VerifyAggregate(_, _, _, _ []byte) error {
	return ErrNotImplemented
}
