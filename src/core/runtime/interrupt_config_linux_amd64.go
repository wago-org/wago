//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package runtime

const interruptSA_RESTORER = 0x04000000

func configureInterruptSigaction(act *interruptSigaction) {
	act.flags |= interruptSA_RESTORER
	act.restorer = addrInterruptSigRestorer()
}

//lint:ignore U1000 entry point referenced from assembly
func interruptSigRestorer()

func addrInterruptSigRestorer() uintptr
