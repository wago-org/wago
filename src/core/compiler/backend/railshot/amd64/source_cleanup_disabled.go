//go:build amd64 && wago_regalloccheck && (tinygo || wago_profile)

package amd64

// Checked cleanup calls this hook even when native source proofs are disabled.
func checkSourceClose(*fn) {}
