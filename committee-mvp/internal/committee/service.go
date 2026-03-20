package committee

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"committee-mvp/internal/share"
	"committee-mvp/internal/wire"
)

// Service orchestrates committee protocol flow for the MVP runtime.
type Service struct {
	cfg *config.NodeConfig
	net p2p.Network
	bls *crypto.BN254BLS

	nodeIndex int

	nonce uint64
	autoSign uint32
	dkgOnce  sync.Once
	dkgReady chan struct{}

	sessions map[string]*signSession
	pending  map[string]pendingSignRequest
	dkgShares map[int]*big.Int
	dealerPubKeys map[int][]byte
	committeePubKey []byte
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

type pendingSignRequest struct {
	message []byte
	epoch   uint64
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
		autoSign:  1,
		dkgReady:  make(chan struct{}),
		sessions:  make(map[string]*signSession),
		pending:   make(map[string]pendingSignRequest),
		dkgShares: make(map[int]*big.Int),
		dealerPubKeys: make(map[int][]byte),
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

	s.startDKG(ctx)
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
	Enabled   bool   `json:"enabled"`
}

type controlResponse struct {
	OK              bool   `json:"ok"`
	Error           string `json:"error,omitempty"`
	CommitteePubKey string `json:"committee_pub_key,omitempty"`
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
	switch req.Action {
	case "submit_sign_request":
		if err := s.SubmitSignRequest(req.SessionID, []byte(req.Message)); err != nil {
			_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: true})
	case "set_auto_sign":
		s.setAutoSign(req.Enabled)
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: true})
	case "sign_session":
		if err := s.SubmitLocalSign(req.SessionID, []byte(req.Message)); err != nil {
			_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: true})
	case "get_committee_pubkey":
		pub, err := s.CommitteePublicKey()
		if err != nil {
			_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: true, CommitteePubKey: hex.EncodeToString(pub)})
	default:
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: false, Error: "unsupported action"})
		return
	}
}

// SubmitSignRequest can be used by coordinator to trigger one signing round.
func (s *Service) SubmitSignRequest(sessionID string, message []byte) error {
	if s.cfg.NodeID != s.cfg.CoordinatorID {
		return fmt.Errorf("only coordinator can submit sign request")
	}
	if !s.isDKGReady() {
		return fmt.Errorf("committee key not ready yet")
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
	case wire.MsgDealShare:
		s.onDealShare(msg)
	case wire.MsgSignRequest:
		s.onSignRequest(msg)
	case wire.MsgSignResponse:
		s.onSignResponse(msg)
	case wire.MsgAggResult:
		s.onAggResult(msg)
	}
}

func (s *Service) startDKG(ctx context.Context) {
	s.dkgOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			if err := s.broadcastDealerShares(); err != nil {
				log.Printf("dkg broadcast shares failed node_id=%s err=%v", s.cfg.NodeID, err)
			}
		}()
	})
}

func (s *Service) broadcastDealerShares() error {
	if s.nodeIndex < 0 || s.nodeIndex >= s.cfg.CommitteeSize {
		return fmt.Errorf("invalid node index %d", s.nodeIndex)
	}

	secret, err := rand.Int(rand.Reader, frCompat{}.Modulus())
	if err != nil {
		return fmt.Errorf("sample dealer secret: %w", err)
	}
	if secret.Sign() == 0 {
		secret.SetInt64(1)
	}
	dealerSigner, err := crypto.NewBN254BLSFromBigInt(secret, s.cfg.DomainSeparation)
	if err != nil {
		return fmt.Errorf("new dealer signer: %w", err)
	}
	dealerPub, err := dealerSigner.PublicKey()
	if err != nil {
		return fmt.Errorf("derive dealer public key: %w", err)
	}

	shares, err := share.Split(secret, s.cfg.Threshold, s.cfg.CommitteeSize)
	if err != nil {
		return fmt.Errorf("split dealer shares: %w", err)
	}

	for _, sh := range shares {
		recipientIdx := int(sh.Index) - 1
		if recipientIdx < 0 || recipientIdx >= len(s.cfg.StaticNodes) {
			continue
		}
		payload, err := wire.EncodePayload(wire.DealSharePayload{
			DealerIndex:    s.nodeIndex,
			RecipientIndex: recipientIdx,
			ShareValue:     sh.Value.String(),
			DealerPubKey:   dealerPub,
		})
		if err != nil {
			return fmt.Errorf("encode deal share: %w", err)
		}
		s.net.Publish(wire.Envelope{
			Type:      wire.MsgDealShare,
			SessionID: "dkg-1",
			Epoch:     1,
			Nonce:     atomic.AddUint64(&s.nonce, 1),
			From:      s.cfg.NodeID,
			To:        s.cfg.StaticNodes[recipientIdx],
			Version:   s.cfg.MessageVersion,
			SentAt:    time.Now().UTC(),
			Payload:   payload,
		})
	}
	return nil
}

func (s *Service) onDealShare(msg wire.Envelope) {
	var payload wire.DealSharePayload
	if err := wire.DecodePayload(msg.Payload, &payload); err != nil {
		log.Printf("decode deal-share failed from=%s err=%v", msg.From, err)
		return
	}
	if payload.RecipientIndex != s.nodeIndex {
		return
	}
	if payload.DealerIndex < 0 || payload.DealerIndex >= s.cfg.CommitteeSize {
		return
	}
	v, ok := new(big.Int).SetString(payload.ShareValue, 10)
	if !ok {
		log.Printf("invalid share value from=%s", msg.From)
		return
	}
	v.Mod(v, frCompat{}.Modulus())

	s.mu.Lock()
	if _, exists := s.dkgShares[payload.DealerIndex]; !exists {
		s.dkgShares[payload.DealerIndex] = v
	}
	if _, exists := s.dealerPubKeys[payload.DealerIndex]; !exists {
		s.dealerPubKeys[payload.DealerIndex] = append([]byte(nil), payload.DealerPubKey...)
	}
	ready := len(s.dkgShares) == s.cfg.CommitteeSize && len(s.dealerPubKeys) == s.cfg.CommitteeSize
	s.mu.Unlock()

	if ready {
		s.finalizeDKG()
	}
}

func (s *Service) finalizeDKG() {
	s.mu.Lock()
	if len(s.committeePubKey) > 0 {
		s.mu.Unlock()
		return
	}
	localShare := big.NewInt(0)
	mod := frCompat{}.Modulus()
	for _, v := range s.dkgShares {
		localShare.Add(localShare, v)
		localShare.Mod(localShare, mod)
	}

	pubs := make([][]byte, 0, len(s.dealerPubKeys))
	for i := 0; i < s.cfg.CommitteeSize; i++ {
		pk, ok := s.dealerPubKeys[i]
		if !ok {
			s.mu.Unlock()
			return
		}
		pubs = append(pubs, pk)
	}
	bitmap := make([]byte, (s.cfg.CommitteeSize+7)/8)
	for i := 0; i < s.cfg.CommitteeSize; i++ {
		bitmap[i/8] |= 1 << uint(i%8)
	}
	aggPub, err := crypto.AggregatePublicKeys(pubs, bitmap)
	if err != nil {
		s.mu.Unlock()
		log.Printf("dkg aggregate committee pubkey failed node_id=%s err=%v", s.cfg.NodeID, err)
		return
	}
	bls, err := crypto.NewBN254BLSFromBigInt(localShare, s.cfg.DomainSeparation)
	if err != nil {
		s.mu.Unlock()
		log.Printf("dkg create signer failed node_id=%s err=%v", s.cfg.NodeID, err)
		return
	}
	s.bls = bls
	s.committeePubKey = aggPub
	readyCh := s.dkgReady
	s.mu.Unlock()

	select {
	case <-readyCh:
	default:
		close(readyCh)
	}
	log.Printf("dkg completed node_id=%s committee_pub_ready=true", s.cfg.NodeID)
}

func (s *Service) isDKGReady() bool {
	select {
	case <-s.dkgReady:
		return true
	default:
		return false
	}
}

// CommitteePublicKey returns the fixed committee-level public key once DKG is complete.
func (s *Service) CommitteePublicKey() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.committeePubKey) == 0 {
		return nil, fmt.Errorf("committee key not ready yet")
	}
	out := make([]byte, len(s.committeePubKey))
	copy(out, s.committeePubKey)
	return out, nil
}

func (s *Service) onSignRequest(msg wire.Envelope) {
	if !s.isDKGReady() {
		log.Printf("ignore sign request before dkg ready session_id=%s node_id=%s", msg.SessionID, s.cfg.NodeID)
		return
	}
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

	if !s.isAutoSignEnabled() {
		s.mu.Lock()
		s.pending[msg.SessionID] = pendingSignRequest{
			message: append([]byte(nil), req.Message...),
			epoch:   msg.Epoch,
		}
		s.mu.Unlock()
		log.Printf("auto signing disabled, request queued session_id=%s node_id=%s", msg.SessionID, s.cfg.NodeID)
		return
	}

	if err := s.publishSignResponse(msg.SessionID, msg.Epoch, req.Message); err != nil {
		log.Printf("publish sign response failed session_id=%s node_id=%s err=%v", msg.SessionID, s.cfg.NodeID, err)
	}
}

func (s *Service) publishSignResponse(sessionID string, epoch uint64, message []byte) error {
	shareSig, err := s.bls.SignShare(message)
	if err != nil {
		return fmt.Errorf("sign share failed: %w", err)
	}
	pub, err := s.bls.PublicKey()
	if err != nil {
		return fmt.Errorf("derive pubkey failed: %w", err)
	}
	respPayload, err := wire.EncodePayload(wire.SignResponsePayload{
		SignerIndex:    s.nodeIndex,
		ShareSignature: shareSig,
		SignerPubKey:   pub,
	})
	if err != nil {
		return fmt.Errorf("encode sign response failed: %w", err)
	}
	s.net.Publish(wire.Envelope{
		Type:      wire.MsgSignResponse,
		SessionID: sessionID,
		Epoch:     epoch,
		Nonce:     atomic.AddUint64(&s.nonce, 1),
		From:      s.cfg.NodeID,
		To:        s.cfg.CoordinatorID,
		Version:   s.cfg.MessageVersion,
		SentAt:    time.Now().UTC(),
		Payload:   respPayload,
	})
	return nil
}

// SubmitLocalSign signs a pending session on this node and sends a SIGN_RESPONSE to coordinator.
func (s *Service) SubmitLocalSign(sessionID string, messageOverride []byte) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	epoch := uint64(1)
	message := append([]byte(nil), messageOverride...)

	s.mu.Lock()
	if pending, ok := s.pending[sessionID]; ok {
		if len(message) == 0 {
			message = append([]byte(nil), pending.message...)
		}
		epoch = pending.epoch
		delete(s.pending, sessionID)
	}
	s.mu.Unlock()

	if len(message) == 0 {
		return fmt.Errorf("no queued request for session %s and message override is empty", sessionID)
	}
	if err := s.publishSignResponse(sessionID, epoch, message); err != nil {
		return err
	}
	log.Printf("manual sign response sent session_id=%s node_id=%s", sessionID, s.cfg.NodeID)
	return nil
}

func (s *Service) setAutoSign(enabled bool) {
	if enabled {
		atomic.StoreUint32(&s.autoSign, 1)
		log.Printf("auto sign enabled node_id=%s", s.cfg.NodeID)
		return
	}
	atomic.StoreUint32(&s.autoSign, 0)
	log.Printf("auto sign disabled node_id=%s", s.cfg.NodeID)
}

func (s *Service) isAutoSignEnabled() bool {
	return atomic.LoadUint32(&s.autoSign) == 1
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
	for _, idx := range indices {
		sigs = append(sigs, state.responses[idx].ShareSignature)
	}
	message := append([]byte(nil), state.message...)
	committeePub := append([]byte(nil), s.committeePubKey...)
	s.mu.Unlock()
	aggSig, err := crypto.AggregateThresholdSignatures(sigs, indices)
	if err != nil {
		log.Printf("aggregate signatures failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	if err := s.bls.VerifyAggregate(committeePub, message, aggSig, bitmap); err != nil {
		log.Printf("aggregate verify failed session_id=%s err=%v", msg.SessionID, err)
		return
	}
	resultPayload, err := wire.EncodePayload(wire.AggResultPayload{
		Bitmap:             bitmap,
		AggregateSig:       aggSig,
		AggregatePublicKey: committeePub,
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
	_ = nodeID
	return nil
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
