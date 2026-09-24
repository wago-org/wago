//go:build (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

const preparedHostFixedEnabled = false
