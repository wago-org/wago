//go:build !wago_regalloccheck && !wago_profile

package shared

// Preserve the exact ordinary semantic fields without diagnostic holders.
type ScalarState scalarState
