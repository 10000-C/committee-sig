package committee

import (
	"bytes"
	"math/big"
	"testing"

	"committee-mvp/internal/config"
	"committee-mvp/internal/crypto"
	"committee-mvp/internal/share"
)

func TestFinalizeDKGBuildsStableCommitteePubKey(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:      "node-1",
		ListenAddr:  "127.0.0.1:3401",
		StaticNodes: []string{"node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7", "node-8"},
		StaticNodeAddrs: map[string]string{
			"node-1": "127.0.0.1:3401",
			"node-2": "127.0.0.1:3402",
			"node-3": "127.0.0.1:3403",
			"node-4": "127.0.0.1:3404",
			"node-5": "127.0.0.1:3405",
			"node-6": "127.0.0.1:3406",
			"node-7": "127.0.0.1:3407",
			"node-8": "127.0.0.1:3408",
		},
		CommitteeSize:    8,
		Threshold:        5,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}

	s := NewService(cfg)
	var expectedPubs [][]byte
	for dealerIdx := 0; dealerIdx < cfg.CommitteeSize; dealerIdx++ {
		secret := bigIntFromInt64(int64(100 + dealerIdx))
		shs, err := share.Split(secret, cfg.Threshold, cfg.CommitteeSize)
		if err != nil {
			t.Fatalf("split dealer %d: %v", dealerIdx, err)
		}
		s.dkgShares[dealerIdx] = shs[0].Value // node-1 gets index=1 share

		d, err := crypto.NewBN254BLSFromBigInt(secret, cfg.DomainSeparation)
		if err != nil {
			t.Fatalf("new dealer signer %d: %v", dealerIdx, err)
		}
		pub, err := d.PublicKey()
		if err != nil {
			t.Fatalf("dealer pub %d: %v", dealerIdx, err)
		}
		s.dealerPubKeys[dealerIdx] = pub
		expectedPubs = append(expectedPubs, pub)
	}

	fullBitmap := make([]byte, (cfg.CommitteeSize+7)/8)
	for i := 0; i < cfg.CommitteeSize; i++ {
		fullBitmap[i/8] |= 1 << uint(i%8)
	}
	expectedCommitteePub, err := crypto.AggregatePublicKeys(expectedPubs, fullBitmap)
	if err != nil {
		t.Fatalf("aggregate expected committee pub: %v", err)
	}

	s.finalizeDKG()
	if !s.isDKGReady() {
		t.Fatalf("dkg not marked ready")
	}
	if !bytes.Equal(s.committeePubKey, expectedCommitteePub) {
		t.Fatalf("committee public key mismatch")
	}
}

func bigIntFromInt64(v int64) *big.Int {
	return new(big.Int).SetInt64(v)
}
