package gc

import "runtime"

// Collector algorithms operate on the shared checked handle and object layout.
// Both native backends use this layout; instruction selection remains separate.
const nativeCollectorFastPaths = runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
