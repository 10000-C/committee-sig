package committee

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"committee-mvp/internal/config"
	"committee-mvp/internal/crypto"
	"committee-mvp/internal/p2p"
	"committee-mvp/internal/wire"
)

// Service orchestrates committee protocol flow for the MVP runtime.
type Service struct {
	cfg *config.NodeConfig
	net p2p.Network
	bls *crypto.BN254BLS

	nodeIndex int

	nonce uint64

	sessions map[string]*signSession
	mu       sync.Mutex
	controlL net.Listener

	wg sync.WaitGroup
}

type signSession struct {
	message       []byte
	responses     map[int]wire.SignResponsePayload
	aggregated    bool
	requestNonce  uint64
}

func NewService(cfg *config.NodeConfig) *Service {
	netw := p2p.NewStaticNetwork(cfg.NodeID, cfg.ListenAddr, cfg.StaticNodeAddrs)
	return NewServiceWithNetwork(cfg, netw)
}

func NewServiceWithNetwork(cfg *config.NodeConfig, netw p2p.Network) *Service {
	if netw == nil {
		netw = p2p.NewStaticNetwork(cfg.NodeID, cfg.ListenAddr, cfg.StaticNodeAddrs)
	}
	idx := indexOfNode(cfg.StaticNodes, cfg.NodeID)
	if idx < 0 {
		idx = 0
	}
	sk := derivePrivateKey(cfg.NodeID)
	bls, err := crypto.NewBN254BLSFromBigInt(sk, cfg.DomainSeparation)
	if err != nil {
		panic(err)
	}
	return &Service{
		cfg:       cfg,
		net:       netw,
		bls:       bls,
		nodeIndex: idx,
		sessions:  make(map[string]*signSession),
	}
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.net.Start(ctx); err != nil {
		return err
	}
	if err := s.startControlServer(ctx); err != nil {
		return err
	}
	log.Printf("committee node started node_id=%s threshold=%d static_peers=%d", s.cfg.NodeID, s.cfg.Threshold, len(s.cfg.StaticNodes))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-s.net.Subscribe():
				if !ok {
					return
				}
				s.handleEnvelope(msg)
			}
		}
	}()
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s.controlL != nil {
		_ = s.controlL.Close()
	}
	if err := s.net.Stop(ctx); err != nil {
		return err
	}
	s.wg.Wait()
	return nil
}

func (s *Service) startControlServer(ctx context.Context) error {
	if s.cfg.ControlAddr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", s.cfg.ControlAddr)
	if err != nil {
		return fmt.Errorf("listen control addr %s: %w", s.cfg.ControlAddr, err)
	}
	s.controlL = ln
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
				}
				return
			}
			s.wg.Add(1)
			go func(c net.Conn) {
				defer s.wg.Done()
				s.handleControlConn(c)
			}(conn)
		}
	}()
	return nil
}

type controlRequest struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type controlResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s *Service) handleControlConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: err.Error()})
		return
	}
	var req controlRequest
	if err := json.Unmarshal(line, &req); err != nil {
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: fmt.Sprintf("decode request: %v", err)})
		return
	}
	if req.Action != "submit_sign_request" {
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: "unsupported action"})
		return
	}
	if err := s.SubmitSignRequest(req.SessionID, []byte(req.Message)); err != nil {
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(controlResponse{OK: true})
}

// SubmitSignRequest can be used by coordinator to trigger one signing round.
func (s *Service) SubmitSignRequest(sessionID string, message []byte) error {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return fmt.Errorf("only coordinator can submit sign request")
	}
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	payload, err := wire.EncodePayload(wire.SignRequestPayload{Message: message})
	if err != nil {
		return fmt.Errorf("encode sign request: %w", err)
	}
	nonce := atomic.AddUint64(&s.nonce, 1)
	s.mu.Lock()
	s.sessions[sessionID] = &signSession{
		message:      append([]byte(nil), message...),
		responses:    make(map[int]wire.SignResponsePayload),
		requestNonce: nonce,
	}
	s.mu.Unlock()
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgSignRequest,
		SessionID: sessionID,
		Epoch:     1,
		Nonce:     nonce,
		From:      s.cfg.NodeID,
		Version:   s.cfg.MessageVersion,
		SentAt:    time.Now().UTC(),
		Payload:   payload,
	})
	return nil
}

func (s *Service) handleEnvelope(msg wire.Envelope) {
	if msg.Version != "" && msg.Version != s.cfg.MessageVersion {
		return
	}
	if msg.To != "" && msg.To != s.cfg.NodeID {
		return
	}
	switch msg.Type {
	case wire.MsgSignRequest:
		s.onSignRequest(msg)
	case wire.MsgSignResponse:
		s.onSignResponse(msg)
	case wire.MsgAggResult:
		s.onAggResult(msg)
	}
}

func (s *Service) onSignRequest(msg wire.Envelope) {
	var req wire.SignRequestPayload
	if err := wire.DecodePayload(msg.Payload, &req); err != nil {
		log.Printf("decode sign request failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	if len(req.Message) == 0 {
		log.Printf("empty sign request message session_id=%s", msg.SessionID)
		return
	}

	if s.cfg.NodeID == s.cfg.CoordinatorID {
		s.mu.Lock()
		if _, ok := s.sessions[msg.SessionID]; !ok {
			s.sessions[msg.SessionID] = &signSession{
				message:      append([]byte(nil), req.Message...),
				responses:    make(map[int]wire.SignResponsePayload),
				requestNonce: msg.Nonce,
			}
		}
		s.mu.Unlock()
	}

	shareSig, err := s.bls.SignShare(req.Message)
	if err != nil {
		log.Printf("sign share failed session_id=%s node_id=%s err=%v", msg.SessionID, s.cfg.NodeID, err)
		return
	}
	pub, err := s.bls.PublicKey()
	if err != nil {
		log.Printf("derive pubkey failed session_id=%s node_id=%s err=%v", msg.SessionID, s.cfg.NodeID, err)
		return
	}
	respPayload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    s.nodeIndex,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		log.Printf("encode sign response failed session_id=%s node_id=%s err=%v", msg.SessionID, s.cfg.NodeID, err)
		return
	}
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgSignResponse,
		SessionID: msg.SessionID,
		Epoch:     msg.Epoch,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		To:        s.cfg.CoordinatorID,
		Version:   s.cfg.MessageVersion,
		SentAt:    time.Now().UTC(),
		Payload:   respPayload,
	})
}

func (s *Service) onSignResponse(msg wire.Envelope) {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return
	}
	s.mu.Lock()
	state, ok := s.sessions[msg.SessionID]
	if !ok {
		s.mu.Unlock()
		log.Printf("unknown sign session response ignored session_id=%s from=%s", msg.SessionID, msg.From)
		return
	}
	if state.aggregated {
		s.mu.Unlock()
		return
	}

	var resp wire.SignResponsePayload
	if err := wire.DecodePayload(msg.Payload, &resp); err != nil {
		s.mu.Unlock()
		log.Printf("decode sign response failed session_id=%s from=%s err=%v", msg.SessionID, msg.From, err)
		return
	}
	if resp.SignerIndex < 0 || resp.SignerIndex >= s.cfg.CommitteeSize {
		s.mu.Unlock()
		log.Printf("invalid signer index session_id=%s from=%s index=%d", msg.SessionID, msg.From, resp.SignerIndex)
		return
	}
	if _, exists := state.responses[resp.SignerIndex]; exists {
		s.mu.Unlock()
		return
	}
	if err := s.bls.VerifyShare(resp.SignerPubKey, state.message, resp.ShareSignature); err != nil {
		s.mu.Unlock()
		log.Printf("invalid share signature session_id=%s signer_index=%d err=%v", msg.SessionID, resp.SignerIndex, err)
		return
	}

	state.responses[resp.SignerIndex] = resp
	if len(state.responses) < s.cfg.Threshold {
		s.mu.Unlock()
		log.Printf("signatures collected session_id=%s progress=%d/%d", msg.SessionID, len(state.responses), s.cfg.Threshold)
		return
	}

	indices := make([]int, 0, len(state.responses))
	for idx := range state.responses {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	bitmap := makeBitmap(indices, s.cfg.CommitteeSize)

	sigs := make([][]byte, 0, len(indices))
	pubs := make([][]byte, 0, len(indices))
	for _, idx := range indices {
		sigs = append(sigs, state.responses[idx].ShareSignature)
		pubs = append(pubs, state.responses[idx].SignerPubKey)
	}
	message := append([]byte(nil), state.message...)
	s.mu.Unlock()
	aggSig, err := s.bls.AggregateSignatures(sigs, bitmap)
	if err != nil {
		log.Printf("aggregate signatures failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	aggPub, err := crypto.AggregatePublicKeys(pubs, bitmap)
	if err != nil {
		log.Printf("aggregate public keys failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	if err := s.bls.VerifyAggregate(aggPub, message, aggSig, bitmap); err != nil {
		log.Printf("aggregate verify failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	resultPayload, err := wire.EncodePayload(wire.AggResultPayload{
		Bitmap:             bitmap,
		AggregateSig:       aggSig,
		AggregatePublicKey: aggPub,
		Message:            message,
	})
	if err != nil {
		log.Printf("encode aggregate result failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	s.mu.Lock()
	state.aggregated = true
	s.mu.Unlock()
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgAggResult,
		SessionID: msg.SessionID,
		Epoch:     msg.Epoch,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		Version:   s.cfg.MessageVersion,
		SentAt:    time.Now().UTC(),
		Payload:   resultPayload,
	})
	log.Printf("aggregate result broadcast session_id=%s signers=%d threshold=%d", msg.SessionID, len(indices), s.cfg.Threshold)
}

func (s *Service) onAggResult(msg wire.Envelope) {
	var result wire.AggResultPayload
	if err := wire.DecodePayload(msg.Payload, &result); err != nil {
		log.Printf("decode aggregate result failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	if err := s.bls.VerifyAggregate(result.AggregatePublicKey, result.Message, result.AggregateSig, result.Bitmap); err != nil {
		log.Printf("aggregate result verify failed session_id=%s from=%s err=%v", msg.SessionID, msg.From, err)
		return
	}
	log.Printf("aggregate result accepted session_id=%s from=%s", msg.SessionID, msg.From)
}

func indexOfNode(nodes []string, nodeID string) int {
	for i, n := range nodes {
		if n == nodeID {
			return i
		}
	}
	return -1
}

func derivePrivateKey(nodeID string) *big.Int {
	h := sha256.Sum256([]byte("committee-sig/mvp/sk/" + nodeID))
	bi := new(big.Int).SetBytes(h[:])
	var e big.Int
	var frOne frCompat
	modulus := frOne.Modulus()
	bi.Mod(bi, modulus)
	if bi.Sign() == 0 {
		bi.SetInt64(1)
	}
	e.Set(bi)
	return &e
}

// frCompat keeps bn254 scalar modulus local to avoid importing fr in this layer.
type frCompat struct{}

func (frCompat) Modulus() *big.Int {
	v, _ := new(big.Int).SetString("21888242871839275222246405745257275088548364400416034343698204186575808495617", 10)
	return v
}

func makeBitmap(indices []int, committeeSize int) []byte {
	if committeeSize <= 0 {
		return []byte{0}
	}
	bitmap := make([]byte, (committeeSize+7)/8)
	for _, idx := range indices {
		if idx < 0 || idx >= committeeSize {
			continue
		}
		byteIdx := idx / 8
		bitIdx := idx % 8
		bitmap[byteIdx] |= 1 << uint(bitIdx)
	}
	return bitmap
}
