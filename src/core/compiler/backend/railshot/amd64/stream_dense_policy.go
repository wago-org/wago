//go:build amd64

package amd64

import "os"

// Preserve the existing regional allocation and dense-rotate encoding policy.
// This is a conservative engine preference, not a new regional planner.
var streamingPreserveDenseEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_PRESERVE_DENSE") == "1"
