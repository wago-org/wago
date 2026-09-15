//go:build arm64

package abi

// ARM64 synchronous parked-host control-frame layout. These offsets are part
// of the native compiler/runtime contract and are shared by sibling backends.
const (
	SyncHostCustomContextOffset = 40
	SyncHostTrampolineOffset    = 176
	SyncHostImportIndexOffset   = 184
	SyncHostArityOffset         = 188
	SyncHostArgsOffset          = 192
	SyncHostResultsOffset       = 704
	SyncHostMaxSlots            = 64
)
