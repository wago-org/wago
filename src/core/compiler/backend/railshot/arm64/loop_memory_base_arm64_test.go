//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func loopMemoryBaseModule(t testing.TB, op byte, grow bool) *wasm.Module {
	b := []byte{1, 2, 0x7f}
	for i, v := range []int32{-1, -2147483648} {
		b = append(b, 0x20, 1, 0x41)
		b = append(b, wasmtest.SLEB32(v)...)
		b = append(b, 0x36, 2)
		b = append(b, wasmtest.ULEB(uint32(0x1004+4*i))...)
	}
	b = append(b, 0x20, 0, 0x21, 2, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x45, 0x0d, 1)
	for _, off := range []uint32{0x1004, 0x1008} {
		b = append(b, 0x20, 3, 0x20, 1, op, 0)
		b = append(b, wasmtest.ULEB(off)...)
		b = append(b, 0x6a, 0x21, 3)
	}
	b = append(b, 0x20, 2, 0x41, 1, 0x6b, 0x21, 2, 0x0c, 0, 0x0b, 0x0b)
	if grow {
		b = append(b, 0x41, 0, 0x40, 0, 0x1a)
	}
	b = append(b, 0x20, 3, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func TestLoopMemoryBaseValues(t *testing.T) {
	old := loopMemoryBaseEnabled
	defer func() { loopMemoryBaseEnabled = old }()
	for _, enabled := range []bool{false, true} {
		loopMemoryBaseEnabled = enabled
		for _, op := range []byte{0x28, 0x2c, 0x2d, 0x2e, 0x2f} {
			m := loopMemoryBaseModule(t, op, false)
			sum := uint32(0x7fffffff)
			switch op {
			case 0x2c, 0x2e:
				sum = 0xffffffff
			case 0x2d:
				sum = 255
			case 0x2f:
				sum = 65535
			}
			for _, guard := range []bool{false, true} {
				for _, n := range []uint64{0, 1, 7} {
					for _, index := range []uint64{0, 1, 123} {
						got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, CompactNative: false}, n, index)
						want := uint64(sum * uint32(n))
						if err != nil || got != want {
							t.Fatalf("on=%v op=%x guard=%v n=%d index=%d got=%x want=%x err=%v", enabled, op, guard, n, index, got, want, err)
						}
					}
				}
			}
		}
	}
}

func TestLoopMemoryBaseAdmissionAndGrowRejection(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := loopMemoryBaseEnabled
	defer func() { loopMemoryBaseEnabled = old }()
	loopMemoryBaseEnabled = true
	for _, grow := range []bool{false, true} {
		m := loopMemoryBaseModule(t, 0x28, grow)
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(m, CompileOptions{Stats: stats, CompactNative: false})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
		hits := stats.Funcs[0].Peephole["loop-memory-base-hit"]
		if grow && hits != 0 || !grow && hits != 2 {
			t.Fatalf("grow=%v hits=%d", grow, hits)
		}
	}
}

func TestLoopMemoryBaseHintEligibility(t *testing.T) {
	old := loopMemoryBaseEnabled
	defer func() { loopMemoryBaseEnabled = old }()
	loopMemoryBaseEnabled = true
	for _, kind := range []string{"ordinary", "shared", "memory64", "table", "call", "bulk"} {
		m := loopMemoryBaseModule(t, 0x28, false)
		switch kind {
		case "shared":
			m.Memories[0].Shared = true
		case "memory64":
			m.Memories[0].Limits.Addr64 = true
		case "table":
			m.Tables = append(m.Tables, wasm.Table{})
		}
		h := newFuncHints(4, 0)
		if kind == "call" {
			h.flags.set(hintHasCall)
		}
		if kind == "bulk" {
			h.flags.set(hintUsesBulkMem)
		}
		s := byteBodyScanner{h: h, m: m, loopMemoryBaseN: 1}
		s.loopMemoryBases[0] = newLoopIntConstCandidate(0x1000, 8, 3)
		s.finishLoopIntConsts()
		want := uint8(0)
		if kind == "ordinary" {
			want = 1
		}
		if s.h.loopIntConstCount != want {
			t.Fatalf("kind=%s addresses=%d want=%d", kind, s.h.loopIntConstCount, want)
		}
	}
}

func TestLoopMemoryBaseCannotAliasIntegerConstant(t *testing.T) {
	f := fn{iconstN: 1}
	f.iconsts[0] = intConstReg{typ: mtI64, bits: 0x1000, reg: X14, address: true}
	if r, ok := f.cachedIntConst(storage{typ: mtI64, cval: 0x1000}); ok {
		t.Fatalf("address pointer borrowed as literal in %v", r)
	}
}

func TestLoopMemoryBasePolicyRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := loopMemoryBaseEnabled
	defer func() { loopMemoryBaseEnabled = old }()
	loopMemoryBaseEnabled = true
	m := loopMemoryBaseModule(t, 0x28, false)
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{Stats: stats, Optimizations: map[string]bool{"loop-memory-base": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		cm.CodeImage.Close()
	}
	if hits := stats.Funcs[0].Peephole["loop-memory-base-hit"]; hits != 0 {
		t.Fatalf("disabled hits=%d", hits)
	}
}

func TestLoopMemoryBaseWideLoads(t *testing.T) {
	old := loopMemoryBaseEnabled
	defer func() { loopMemoryBaseEnabled = old }()
	values := []uint64{0x80000000ffffffff, 0x8000000000000080}
	for _, op := range []byte{0x29, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35} {
		b := []byte{2, 1, 0x7f, 1, 0x7e}
		for i, v := range values {
			b = append(b, 0x20, 1, 0x42)
			b = append(b, wasmtest.SLEB64(int64(v))...)
			b = append(b, 0x37, 0)
			b = append(b, wasmtest.ULEB(uint32(0x1008+8*i))...)
		}
		b = append(b, 0x20, 0, 0x21, 2, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x45, 0x0d, 1)
		for _, off := range []uint32{0x1008, 0x1010} {
			b = append(b, 0x20, 3, 0x20, 1, op, 0)
			b = append(b, wasmtest.ULEB(off)...)
			b = append(b, 0x7c, 0x21, 3)
		}
		b = append(b, 0x20, 2, 0x41, 1, 0x6b, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 3, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, b)
		var sum uint64
		for _, v := range values {
			switch op {
			case 0x29:
				sum += v
			case 0x30:
				sum += uint64(int64(int8(v)))
			case 0x31:
				sum += uint64(uint8(v))
			case 0x32:
				sum += uint64(int64(int16(v)))
			case 0x33:
				sum += uint64(uint16(v))
			case 0x34:
				sum += uint64(int64(int32(v)))
			case 0x35:
				sum += uint64(uint32(v))
			}
		}
		for _, on := range []bool{false, true} {
			loopMemoryBaseEnabled = on
			for _, guard := range []bool{false, true} {
				for _, n := range []uint64{0, 1, 7} {
					for _, index := range []uint64{0, 1, 123} {
						got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, CompactNative: false}, n, index)
						if err != nil || got != sum*n {
							t.Fatalf("op=%x on=%v guard=%v n=%d index=%d got=%x want=%x err=%v", op, on, guard, n, index, got, sum*n, err)
						}
					}
				}
			}
		}
	}
}
