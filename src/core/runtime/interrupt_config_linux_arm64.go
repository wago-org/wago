//go:build linux && arm64 && !tinygo && !wago_target_tinygo

package runtime

// Linux/arm64 supplies the signal-return trampoline; SA_RESTORER is an x86
// kernel ABI detail and must not be installed here.
func configureInterruptSigaction(*interruptSigaction) {}
