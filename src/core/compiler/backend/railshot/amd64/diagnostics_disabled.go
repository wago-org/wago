//go:build amd64 && !wago_profile && !wago_gcstats && !wago_codegenstats

package amd64

// diagnosticsEnabled gates compiler measurement and reporting in ordinary builds.
const diagnosticsEnabled = false
