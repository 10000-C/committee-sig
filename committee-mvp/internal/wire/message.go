package wire

import "time"

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
