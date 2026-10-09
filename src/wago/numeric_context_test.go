//go:build (linux || darwin) && (amd64 || arm64)

package wago

func numericContextDetached(in *Instance) bool {
	p := in.eng.PreparedScalarHost()
	return p != nil && p.DetachedNumericContext()
}
