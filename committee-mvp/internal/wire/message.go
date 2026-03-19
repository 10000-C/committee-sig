package wire

import (
	"encoding/json"
	"time"
)

type MessageType uint8

const (
	MsgHello MessageType = iota + 1
	MsgDealShare
	MsgSignRequest
	MsgSignResponse
	MsgAggResult
)

// Envelope is the common wire frame used by the protocol.
type Envelope struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"session_id"`
	Epoch     uint64      `json:"epoch"`
	Nonce     uint64      `json:"nonce"`
	From      string      `json:"from"`
	To        string      `json:"to"`
	Version   string      `json:"version"`
	SentAt    time.Time   `json:"sent_at"`
	Payload   []byte      `json:"payload"`
}

type SignRequestPayload struct {
	Message []byte `json:"message"`
}

type SignResponsePayload struct {
	SignerIndex    int    `json:"signer_index"`
	ShareSignature []byte `json:"share_signature"`
	SignerPubKey   []byte `json:"signer_pub_key"`
}

type AggResultPayload struct {
	Bitmap            []byte `json:"bitmap"`
	AggregateSig      []byte `json:"aggregate_signature"`
	AggregatePublicKey []byte `json:"aggregate_public_key"`
	Message           []byte `json:"message"`
}

func EncodePayload(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func DecodePayload(b []byte, v interface{}) error {
	return json.Unmarshal(b, v)
}
