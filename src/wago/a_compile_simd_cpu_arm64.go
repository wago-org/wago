//go:build arm64

package wago

func architectureSupportsSIMD() bool { return true }

func architectureAMD64BitCountFeatures() uint8 { return 0 }
