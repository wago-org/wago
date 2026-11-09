//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBlockEntryInitializationPrefixFacts(t *testing.T) {
	old := blockEntryInitEnabled
	defer func() { blockEntryInitEnabled = old }()
	for _, tc := range []struct {
		name string
		body []byte
		mask uint64
	}{
		{"block", []byte{0x02, 0x40, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 1},
		{"nested-block", []byte{0x02, 0x40, 0x02, 0x40, 0x41, 7, 0x21, 0, 0x0b, 0x0b, 0x0b}, 1},
		{"read-first", []byte{0x02, 0x40, 0x20, 0, 0x1a, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"branch-first", []byte{0x02, 0x40, 0x41, 1, 0x0d, 0, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"write-before-branch", []byte{0x02, 0x40, 0x41, 7, 0x21, 0, 0x41, 1, 0x0d, 0, 0x0b, 0x0b}, 1},
		{"conditional", []byte{0x41, 1, 0x04, 0x40, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"loop", []byte{0x03, 0x40, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"return", []byte{0x02, 0x40, 0x0f, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"table-branch", []byte{0x02, 0x40, 0x41, 0, 0x0e, 0, 0, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"unreachable", []byte{0x02, 0x40, 0, 0x41, 7, 0x21, 0, 0x0b, 0x0b}, 0},
		{"cast-branch", []byte{0x02, 0x6e, 0xd0, 0x6e, 0xfb, 0x18, 3, 0, 0x6e, 0x6d, 0x1a, 0x41, 7, 0x21, 0, 0xd0, 0x6e, 0x0b, 0x1a, 0x0b}, 0},
		{"cast-fail-branch", []byte{0x02, 0x6e, 0xd0, 0x6e, 0xfb, 0x19, 3, 0, 0x6e, 0x6d, 0x1a, 0x41, 7, 0x21, 0, 0xd0, 0x6e, 0x0b, 0x1a, 0x0b}, 0},
	} {
		for _, enabled := range []bool{false, true} {
			blockEntryInitEnabled = enabled
			h, err := scanBodyBytes(tc.body, 1, 0, 0)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			want := uint64(0)
			if enabled {
				want = tc.mask
			}
			if h.entryInitialized != want {
				t.Fatalf("%s enabled%v: mask%x want%x", tc.name, enabled, h.entryInitialized, want)
			}
		}
	}
}

func TestBlockEntryInitializationBoundedTypeMask(t *testing.T) {
	m := blockEntryInitFixture(t, 0x7f, "block")
	m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.F64}, {Count: 1, Type: wasm.I32}, {Count: 1, Type: wasm.I64}, {Count: 1, Type: wasm.V128}}
	s := byteBodyScanner{m: m}
	for i, want := range []bool{false, false, true, true, false, false} {
		if got := s.blockEntryLocalEligible(uint32(i)); got != want {
			t.Fatalf("local%d eligible%v want%v", i, got, want)
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		s.entryBlockTypesReady, s.entryBlockGP = false, 0
		s.blockEntryLocalEligible(2)
	}); n != 0 {
		t.Fatalf("type mask allocated %g times", n)
	}
	m.Code[0].Locals.Runs = append(make([]wasm.LocalRun, 64), wasm.LocalRun{Count: 1, Type: wasm.I32})
	s = byteBodyScanner{m: m}
	if s.blockEntryLocalEligible(1) {
		t.Fatal("zero-count run budget exceeded")
	}
	m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: ^uint32(0), Type: wasm.I32}}
	s = byteBodyScanner{m: m}
	if !s.blockEntryLocalEligible(63) || s.blockEntryLocalEligible(64) {
		t.Fatal("large declared run escaped64-local mask")
	}
	s = byteBodyScanner{m: m, selfIdx: 99}
	if s.blockEntryLocalEligible(1) {
		t.Fatal("unknown signature accepted")
	}
}

func blockEntryInitFixture(t *testing.T, typ byte, variant string) *wasm.Module {
	body := []byte{1, 8, typ, 0x02, 0x40}
	if variant == "nested" {
		body = append(body, 0x02, 0x40)
	}
	if variant == "branch-before" {
		body = append(body, 0x20, 0, 0x0d, 0)
	}
	if variant == "conditional" {
		body = append(body, 0x20, 0, 0x04, 0x40)
	}
	if variant == "read-first" {
		body = append(body, 0x41, 0, 0x20, 1)
		body = appendBlockInitStore(body, typ)
	}
	for i := byte(1); i <= 8; i++ {
		switch typ {
		case 0x7f:
			body = append(body, 0x41, i)
		case 0x7e:
			body = append(body, 0x42, i)
		case 0x7d:
			body = append(body, 0x43)
			body = binary.LittleEndian.AppendUint32(body, 0x80000000|uint32(i))
		case 0x7c:
			body = append(body, 0x44)
			body = binary.LittleEndian.AppendUint64(body, 0x7ff0000000000000|uint64(i))
		case 0x7b:
			body = append(body, 0xfd, 0x0c)
			body = append(body, bytes.Repeat([]byte{i}, 16)...)
		}
		body = append(body, 0x21, i)
	}
	if variant == "branch-after" {
		body = append(body, 0x20, 0, 0x0d, 0)
	}
	if variant == "nested" || variant == "conditional" {
		body = append(body, 0x0b)
	}
	body = append(body, 0x0b, 0x41, 0, 0x10, 1) // real non-inlined call after the join
	for i := byte(1); i <= 8; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(16*int32(i))...)
		body = append(body, 0x20, i)
		body = appendBlockInitStore(body, typ)
	}
	body = append(body, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
	m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x0b}})
	return m
}

func appendBlockInitStore(body []byte, typ byte) []byte {
	switch typ {
	case 0x7f:
		return append(body, 0x36, 2, 0)
	case 0x7e:
		return append(body, 0x37, 3, 0)
	case 0x7d:
		return append(body, 0x38, 2, 0)
	case 0x7c:
		return append(body, 0x39, 3, 0)
	default:
		return append(body, 0xfd, 0x0b, 4, 0)
	}
}

func TestBlockEntryInitializationNativeState(t *testing.T) {
	old := blockEntryInitEnabled
	defer func() { blockEntryInitEnabled = old }()
	for _, typ := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		for _, variant := range []string{"block", "nested", "branch-before", "branch-after", "conditional", "read-first"} {
			m := blockEntryInitFixture(t, typ, variant)
			for _, condition := range []uint64{0, 1} {
				initial := bytes.Repeat([]byte{0x9b}, 65536)
				want := append([]byte(nil), initial...)
				assigned := !(variant == "branch-before" && condition == 1 || variant == "conditional" && condition == 0)
				width := 4
				if typ == 0x7c || typ == 0x7e {
					width = 8
				} else if typ == 0x7b {
					width = 16
				}
				if variant == "read-first" {
					clear(want[:width])
				}
				for i := 1; i <= 8; i++ {
					b := want[16*i : 16*i+width]
					clear(b)
					if assigned {
						switch typ {
						case 0x7f:
							binary.LittleEndian.PutUint32(b, uint32(i))
						case 0x7e:
							binary.LittleEndian.PutUint64(b, uint64(i))
						case 0x7d:
							binary.LittleEndian.PutUint32(b, 0x80000000|uint32(i))
						case 0x7c:
							binary.LittleEndian.PutUint64(b, 0x7ff0000000000000|uint64(i))
						default:
							copy(b, bytes.Repeat([]byte{byte(i)}, 16))
						}
					}
				}
				for _, enabled := range []bool{false, true} {
					blockEntryInitEnabled = enabled
					for _, compact := range []bool{false, true} {
						opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false}}
						got, err := runLinearSpanMemory(t, m, opts, initial, condition)
						if err != nil || !bytes.Equal(got, want) {
							t.Fatalf("type%x variant%s cond%d enabled%v compact%v err%v", typ, variant, condition, enabled, compact, err)
						}
					}
				}
			}
		}
	}
}

func TestBlockEntryInitializationElidesTypedZeroes(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := blockEntryInitEnabled
	defer func() { blockEntryInitEnabled = old }()
	for _, typ := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b} {
		m := blockEntryInitFixture(t, typ, "nested")
		for _, enabled := range []bool{false, true} {
			blockEntryInitEnabled = enabled
			stats := compileWithStats(t, m, false)
			want := 0
			if enabled && (typ == 0x7f || typ == 0x7e) {
				want = 8
			}
			if got := stats.Funcs[0].Peephole["entry-init-elide"]; got != want {
				t.Fatalf("type%x enabled%v: elisions%d want%d", typ, enabled, got, want)
			}
		}
	}
}
