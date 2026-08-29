// Package ai contains the concrete AI provider and indexing adapters.
package ai

import "gofile/internal/port"

// Analysis and Provider are kept as aliases for callers that work directly
// with this adapter package. The contracts are owned by the application port
// package.
type Analysis = port.Analysis
type Provider = port.Provider
