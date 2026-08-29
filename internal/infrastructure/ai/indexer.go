package ai

import "gofile/internal/port"

// Doc and Indexer are adapter aliases; the application contract lives in
// internal/port so the service layer does not depend on Typesense.
type Doc = port.Doc
type Indexer = port.Indexer
