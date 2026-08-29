package storage

import "fmt"

// ErrPresignNotSupported is returned by local storage for S3-only operations.
var ErrPresignNotSupported = fmt.Errorf("presigned URL not supported for local storage")

// Contracts (Storage, CompletePart) are owned by internal/port. This package
// provides concrete adapters (Local, MinIO) that implement port.Storage.
