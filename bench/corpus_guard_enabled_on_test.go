//go:build wago_guardpage && ((linux && (amd64 || arm64)) || (darwin && arm64))

package wagobench

func corpusGuardEnabled() bool { return true }
