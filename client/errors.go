package client

import "errors"

var (
	// ErrAuthRequired reports that the ADB peer requires authentication and no
	// supplied credential completed authorization.
	ErrAuthRequired = errors.New("adb authentication required")

	// ErrUnsupported reports that the requested operation is not supported yet.
	ErrUnsupported = errors.New("adb operation unsupported")

	// ErrDestinationExists reports that a pull destination already exists and
	// overwrite was not explicitly requested.
	ErrDestinationExists = errors.New("adb destination exists")
)
