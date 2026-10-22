//go:build ((linux || darwin) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

func numericContextDetached(in *Instance) bool {
	p := in.eng.PreparedScalarHost()
	return p != nil && p.DetachedNumericContext()
}
