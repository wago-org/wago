//go:build arm64

package wago

func architectureSupportsSIMD() bool { return true }
func architectureSupportsBMI2() bool { return false }
