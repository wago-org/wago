//go:build amd64

package abi

// AMD64 synchronous parked-host control-frame layout. These offsets are part
// of the native compiler/runtime contract and are shared by sibling backends.
const (
	SyncHostCustomContextOffset = 40
	SyncHostTrampolineOffset    = 56
	SyncHostImportIndexOffset   = 64
	SyncHostArityOffset         = 68
	SyncHostArgsOffset          = 72
	SyncHostResultsOffset       = 584
	SyncHostMaxSlots            = 64
)
