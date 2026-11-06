//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Leading typed literal assignments execute before any read, branch, or call.
// Only those declared locals can omit their synthetic zero initialization.
// Stop at the first other instruction; later or conditional writes prove nothing.
func inlineEntryConstantLocals(body []byte, params int, types []machineType) uint64 {
	r := wasm.NewReader(body)
	var initialized uint64
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			break
		}
		if op == 0x01 {
			continue
		}
		var typ machineType
		switch op {
		case 0x41:
			if _, err := r.I32(); err != nil {
				return initialized
			}
			typ = mtI32
		case 0x42:
			if _, err := r.I64(); err != nil {
				return initialized
			}
			typ = mtI64
		default:
			return initialized
		}
		op, err = r.Byte()
		if err != nil || op != 0x21 && op != 0x22 {
			return initialized
		}
		x, err := r.U32()
		if err != nil || uint64(x) >= uint64(len(types)) || types[x] != typ {
			return initialized
		}
		if x < 64 && int(x) >= params {
			initialized |= uint64(1) << x
		}
	}
	return initialized
}
