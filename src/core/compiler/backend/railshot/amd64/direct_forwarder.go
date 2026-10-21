//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var directForwarderEnabled = true

const (
	maxDirectForwarderHops = 4
	maxDirectForwarderBody = 128
)

const (
	forwarderTerminal = iota
	forwarderMatch
	forwarderBlocked
)

// directForwarderTarget only changes direct call edges. The original function
// bodies, exports and function-reference identities remain available. Require a
// complete bounded chain; a cycle, import or type-changing forwarding edge is
// never partially collapsed. No runtime type check or root mask is removed.
func directForwarderTarget(m *wasm.Module, imported int, first uint32) (uint32, bool) {
	var seen [maxDirectForwarderHops + 1]uint32
	current := first
	for hops := 0; hops <= maxDirectForwarderHops; hops++ {
		for _, prior := range seen[:hops] {
			if current == prior {
				return first, false
			}
		}
		seen[hops] = current
		next, kind := directForwarderNext(m, imported, current)
		if kind == forwarderTerminal {
			return current, hops != 0
		}
		if kind == forwarderBlocked || hops == maxDirectForwarderHops {
			return first, false
		}
		current = next
	}
	return first, false
}

func directForwarderNext(m *wasm.Module, imported int, index uint32) (uint32, int) {
	local := int(index) - imported
	if local < 0 || local >= len(m.Code) {
		return 0, forwarderBlocked
	}
	body := &m.Code[local]
	if len(body.Locals.Runs) != 0 || len(body.BodyBytes) > maxDirectForwarderBody {
		return 0, forwarderTerminal
	}
	ft, ok := m.LocalFuncType(local)
	if !ok {
		return 0, forwarderBlocked
	}
	r := wasm.ReaderFrom(body.BodyBytes)
	for param := range ft.Params {
		op, err := r.Byte()
		if err != nil || op != 0x20 {
			return 0, forwarderTerminal
		}
		arg, err := r.U32()
		if err != nil || arg != uint32(param) {
			return 0, forwarderTerminal
		}
	}
	op, err := r.Byte()
	if err != nil || op != 0x10 {
		return 0, forwarderTerminal
	}
	next, err := r.U32()
	if err != nil {
		return 0, forwarderBlocked
	}
	op, err = r.Byte()
	if err != nil || op != 0x0b || r.BytesLeft() != 0 {
		return 0, forwarderTerminal
	}
	if next < uint32(imported) {
		return 0, forwarderBlocked
	}
	target, ok := m.LocalFuncType(int(next) - imported)
	if !ok || !equalForwarderTypes(ft.Params, target.Params) || !equalForwarderTypes(ft.Results, target.Results) {
		return 0, forwarderBlocked
	}
	return next, forwarderMatch
}

func equalForwarderTypes(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !wasm.EqualValType(a[i], b[i]) {
			return false
		}
	}
	return true
}
