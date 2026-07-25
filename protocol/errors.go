package protocol

import "errors"

var (
	// ErrAuthRequired reports that the ADB peer requires authentication that is
	// not implemented by adb-go yet.
	ErrAuthRequired = errors.New("adb authentication required")
)

// AuthRequiredError reports an AUTH packet received during connection
// handshake. Callers can inspect Message to observe the raw AUTH packet.
type AuthRequiredError struct {
	Message Message
}

func (e *AuthRequiredError) Error() string {
	return ErrAuthRequired.Error()
}

func (e *AuthRequiredError) Is(target error) bool {
	return target == ErrAuthRequired
}
