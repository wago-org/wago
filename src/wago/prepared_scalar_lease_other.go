//go:build !arm64 || (!linux && !darwin && !windows)

package wago

const preparedScalarInPlaceLease = false
