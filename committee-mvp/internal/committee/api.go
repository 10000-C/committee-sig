package committee

import "context"

// API defines the externally consumed committee protocol operations.
type API interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SubmitSignRequest(sessionID string, message []byte) error
}
