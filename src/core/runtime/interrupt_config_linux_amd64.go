//go:build linux && amd64 && !tinygo

package runtime

const interruptSA_RESTORER = 0x04000000

func configureInterruptSigaction(act *interruptSigaction) {
	act.flags |= interruptSA_RESTORER
	act.restorer = addrInterruptSigRestorer()
}

func interruptSigRestorer()
func addrInterruptSigRestorer() uintptr
