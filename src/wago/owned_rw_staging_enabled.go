//go:build linux && amd64 && wago_experimental_owned_rw_staging

package wago

import "os"

var ownedRWStaging = parseOwnedRWStaging(os.Getenv("WAGO_AMD64_OWNED_RW_STAGING"))

func parseOwnedRWStaging(value string) bool {
	switch value {
	case "", "0":
		return false
	case "1":
		return true
	default:
		panic("WAGO_AMD64_OWNED_RW_STAGING must be 0 or 1")
	}
}
func configureOwnedRWStaging(opts *railshotCompileOptions) {
	configureOwnedRWStagingMode(opts, ownedRWStaging)
}
func configureOwnedRWStagingMode(opts *railshotCompileOptions, enabled bool) {
	// Restrict to explicitly resolved serial compilation. The established backend
	// CodeImage/Take owner transfers RW storage; first activation still seals RX.
	if enabled && opts.Workers == 1 {
		opts.DeferCodeMapping = false
	}
}
