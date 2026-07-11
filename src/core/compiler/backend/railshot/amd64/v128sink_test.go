//go:build linux && amd64

package amd64

import "testing"

// TestV128LocalSinkFires checks that `local.set $x (v128bin (local.get $x) …)`
// into a pinned v128 local sinks into one 3-operand op (the v128-local-sink
// peephole), and that WAGO_AMD64_NO_V128_SINK-style gating leaves it available.
func TestV128LocalSinkFires(t *testing.T) {
	// (func (local $a v128) (local $b v128)
	//   (local.set $a (v128.and (local.get $a) (local.get $b))))
	body := []byte{
		0x01, 0x02, 0x7b, // 1 local group: 2 × v128
		0x20, 0x00, // local.get $a
		0x20, 0x01, // local.get $b
		0xfd, 0x4e, // v128.and
		0x21, 0x00, // local.set $a
		0x0b, // end
	}
	m := mod1(t, nil, nil, body)
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["v128-local-sink"] == 0 {
		t.Fatalf("v128-local-sink did not fire; peepholes = %v", s.Peephole)
	}
}
