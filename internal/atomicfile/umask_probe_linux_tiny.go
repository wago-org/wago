//go:build linux && tinygo && wago_lean && wago_minimal

package atomicfile

import "io/fs"

// ApplyUmask is used by the build command, which is deliberately absent from
// the TinyGo minimal runtime. Reject an unexpected request rather than linking
// its descriptor-pinned probe machinery or silently changing file semantics.
func probeUmaskMode(string, fs.FileMode, bool) (fs.FileMode, error) {
	return 0, newError("atomic umask probing is unavailable")
}
