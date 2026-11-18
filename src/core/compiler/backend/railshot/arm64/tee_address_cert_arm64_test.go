//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestTeeAddressCertificateSemanticVersion(t *testing.T) {
	old := teeAddressCertEnabled
	teeAddressCertEnabled = true
	t.Cleanup(func() { teeAddressCertEnabled = old })
	f := fn{a: &enc.Asm{}, policy: currentCodegenPolicy(), boundsFacts: true, nLocals: 1, localType: []machineType{mtI32}, localSlot: []uint32{0}}
	off := f.localOff(0)
	f.a.Store64(X9, SP, uint32(off))
	f.frameForwardEnd = uint32(f.a.Len())
	f.frameForwardOff = off
	f.frameForwardReg = X9
	f.frameForwardLocal = 0
	f.frameForwardLocalLive = true
	e := elem{st: storage{kind: stReg, typ: mtI32, reg: X9}}
	if x, ok := f.teeAddressLocal(&e); !ok || x != 0 {
		t.Fatal("unchanged store lost local identity")
	}
	end := f.frameForwardEnd
	f.invalidateLocalWriteFacts(0)
	if _, ok := f.teeAddressLocal(&e); ok {
		t.Fatal("semantic write kept stale identity")
	}
	if f.frameForwardEnd != end {
		t.Fatal("semantic identity invalidation changed ordinary frame forwarding")
	}
}

func TestTeeAddressCertificateMemoryJoinsAndTraps(t *testing.T) {
	old := teeAddressCertEnabled
	t.Cleanup(func() { teeAddressCertEnabled = old })
	for _, mode := range []string{"both-arms", "wider", "changed-arm", "stale-stack", "loop-write"} {
		body := []byte{1, 20, 0x7f}
		// Keep the assigned pointer outside the permanent register pool.
		for x := byte(4); x < 23; x++ {
			for range 64 {
				body = append(body, 0x20, x, 0x1a)
			}
		}
		body = append(body, 0x20, 0, 0x22, 23)
		if mode == "stale-stack" {
			body = append(body, 0x20, 3, 0x21, 23)
		}
		body = append(body, 0x28, 0, 0, 0x1a, 0x41, 32, 0x41, 7, 0x36, 0, 0)
		if mode == "loop-write" {
			body = append(body, 0x41, 2, 0x21, 4, 0x03, 0x40, 0x20, 23, 0x20, 1, 0x36, 0, 0, 0x20, 3, 0x21, 23, 0x20, 4, 0x41, 1, 0x6b, 0x22, 4, 0x0d, 0, 0x0b)
		} else {
			body = append(body, 0x20, 2, 0x04, 0x40)
			if mode == "changed-arm" {
				body = append(body, 0x20, 3, 0x21, 23)
			}
			body = append(body, 0x20, 23, 0x20, 1)
			if mode == "wider" {
				body = append(body, 0xad, 0x37, 0, 0)
			} else {
				body = append(body, 0x36, 0, 0)
			}
			body = append(body, 0x05, 0x20, 23, 0x20, 1, 0x3b, 0, 2, 0x0b)
		}
		body = append(body, 0x41, 40, 0x41, 11, 0x36, 0, 0, 0x41, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, addr := range []uint64{0, 1, 65528, 65529, 65532, 65533, 65535, 65536, 0xffffffff, 0xdeadbeef0000fffc} {
			for _, condition := range []uint64{0, 1, 0xdeadbeef00000000, 0xdeadbeef00000001} {
				for _, other := range []uint64{8, 65535} {
					value := uint64(0x1234567889abcdef)
					want := bytes.Repeat([]byte{0xa5}, 65536)
					ptr := uint32(addr)
					trap := uint64(ptr)+4 > 65536
					if !trap {
						binary.LittleEndian.PutUint32(want[32:], 7)
						if mode == "stale-stack" {
							ptr = uint32(other)
						}
						store := func(off, size uint32) {
							if trap {
								return
							}
							at := uint64(ptr) + uint64(off)
							if at+uint64(size) > 65536 {
								trap = true
								return
							}
							switch size {
							case 2:
								binary.LittleEndian.PutUint16(want[at:], uint16(value))
							case 4:
								binary.LittleEndian.PutUint32(want[at:], uint32(value))
							case 8:
								binary.LittleEndian.PutUint64(want[at:], uint64(uint32(value)))
							}
						}
						if mode == "loop-write" {
							store(0, 4)
							ptr = uint32(other)
							store(0, 4)
						} else if uint32(condition) != 0 {
							if mode == "changed-arm" {
								ptr = uint32(other)
							}
							size := uint32(4)
							if mode == "wider" {
								size = 8
							}
							store(0, size)
						} else {
							store(2, 2)
						}
						if !trap {
							binary.LittleEndian.PutUint32(want[40:], 11)
						}
					}
					for _, enabled := range []bool{false, true} {
						teeAddressCertEnabled = enabled
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"interval-region-pins": false, "memory-min-bounds": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runLinearSpanMemory(t, m, opts, bytes.Repeat([]byte{0xa5}, 65536), addr, value, condition, other)
						if (err != nil) != trap || !bytes.Equal(got, want) {
							t.Fatalf("mode=%s address=%x cond=%x other=%d enabled=%v err=%v wantTrap=%v memoryEqual=%v", mode, addr, condition, other, enabled, err, trap, bytes.Equal(got, want))
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["tee-address-certificate"] > 0) != (enabled && mode != "stale-stack") {
							t.Fatalf("mode=%s enabled=%v stats=%v", mode, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
