//go:build amd64 && !tinygo && !wago_profile

package amd64

import "os"

var streamingRestoreReuseEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_RESTORE_REUSE") == "1"

// canReuseRestoredLocal excludes every independently observable alias and every
// register/backing ownership contract. Canonicalization has already captured
// old snapshots and committed the current frame image before restore runs.
func (s *streamState) canReuseRestoredLocal(local int, id streamID) bool {
	if id == 0 {
		return false
	}
	n := s.node(id)
	if n.kind != streamBorrow || int(n.slot) != local || n.home != uint16(local+1) || n.refs != 1 {
		return false
	}
	_, selected := s.localHomeReg(local)
	return !selected
}
