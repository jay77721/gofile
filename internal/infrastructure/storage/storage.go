package storage

import (
	"fmt"
	"gofile/internal/port"
)

// ErrPresignNotSupported is returned by local storage for S3-only operations.
var ErrPresignNotSupported = fmt.Errorf("presigned URL not supported for local storage")

// CompletePart and Storage remain adapter aliases for compatibility. The
// contracts themselves are owned by internal/port.
type CompletePart = port.CompletePart
type Storage = port.Storage
