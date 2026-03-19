package committee

import (
	"context"
	"testing"
	"time"

	"committee-mvp/internal/config"
	"committee-mvp/internal/crypto"
	"committee-mvp/internal/wire"
)

func TestCoordinatorAggregatesAfterThresholdResponses(t *testing.T) {
	cfg := &config.NodeConfig{
		NodeID:           "node-1",
		ListenAddr:       "127.0.0.1:3401",
		StaticNodes:      []string{"node-1", "node-2", "node-3", "node-4", "node-5", "node-6", "node-7", "node-8"},
		CommitteeSize:    8,
		Threshold:        5,
		CoordinatorID:    "node-1",
		DomainSeparation: "committee-sig/mvp/v1",
		MessageVersion:   "v1",
	}

	svc := NewService(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start service: %v", err)
	}
	defer func() {
		if err := svc.Stop(context.Background()); err != nil {
			t.Fatalf("stop service: %v", err)
		}
	}()

	sessionID := "sess-1"
	message := []byte("test-sign-round")
	if err := svc.SubmitSignRequest(sessionID, message); err != nil {
		t.Fatalf("submit sign request: %v", err)
	}

	for _, nodeID := range []string{"node-2", "node-3", "node-4", "node-5"} {
		signer, err := crypto.NewBN254BLSFromBigInt(derivePrivateKey(nodeID), cfg.DomainSeparation)
		if err != nil {
			t.Fatalf("new signer %s: %v", nodeID, err)
		}
		sig, err := signer.SignShare(message)
		if err != nil {
			t.Fatalf("sign share %s: %v", nodeID, err)
		}
		pk, err := signer.PublicKey()
		if err != nil {
			t.Fatalf("public key %s: %v", nodeID, err)
		}
		payload, err := wire.EncodePayload(wire.SignResponsePayload{
			SignerIndex:    indexOfNode(cfg.StaticNodes, nodeID),
			ShareSignature: sig,
			SignerPubKey:   pk,
		})
		if err != nil {
			t.Fatalf("encode payload %s: %v", nodeID, err)
		}
		svc.net.Publish(wire.Envelope{
			Type:      wire.MsgSignResponse,
			SessionID: sessionID,
			Epoch:     1,
			Nonce:     100,
			From:      nodeID,
			To:        cfg.CoordinatorID,
			Version:   cfg.MessageVersion,
			SentAt:    time.Now().UTC(),
			Payload:   payload,
		})
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		state, ok := svc.sessions[sessionID]
		if ok && state.aggregated {
			if len(state.responses) < cfg.Threshold {
				t.Fatalf("responses below threshold: got=%d want>=%d", len(state.responses), cfg.Threshold)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for aggregation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
