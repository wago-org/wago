//go:build arm64 && (wago_profile || wago_gcstats || wago_codegenstats)

package arm64

// diagnosticsEnabled gates compiler measurement and reporting in ordinary builds.
const diagnosticsEnabled = true
