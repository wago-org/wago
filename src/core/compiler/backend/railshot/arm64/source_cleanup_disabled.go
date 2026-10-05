//go:build arm64 && wago_regalloccheck && (tinygo || wago_profile)

package arm64

func checkSourceClose(*fn) {}
