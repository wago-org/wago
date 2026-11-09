//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func residualStoreFixture(t testing.TB, delta, offset [2]uint32, shift uint32) *wasm.Module {
	body := []byte{1, 1, 0x7f}
	c := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	get := func(i byte) { body = append(body, 0x20, i) }
	store := func(i int) { body = append(body, 0x36, 0); body = append(body, wasmtest.ULEB(offset[i])...) }
	c(48)
	c(42)
	body = append(body, 0x3a, 0, 0)
	body = append(body, 0x02, 0x40)
	get(0)
	body = append(body, 0x45, 0x0d, 0, 0x03, 0x40)
	get(3)
	c(delta[0])
	body = append(body, 0x6a)
	get(1)
	get(1)
	c(13)
	body = append(body, 0x76, 0x73)
	c(0x85ebca6b)
	body = append(body, 0x6c, 0x22, 4)
	c(shift)
	body = append(body, 0x75)
	store(0)
	get(3)
	c(delta[1])
	body = append(body, 0x6a)
	get(4) // This old value must survive replacement of the mutable cell.
	get(4)
	c(1)
	body = append(body, 0x6a, 0x21, 4)
	c(0x87654321)
	body = append(body, 0x6a)
	c(3)
	body = append(body, 0x75)
	store(1)
	get(2)
	get(4)
	body = append(body, 0x73)
	c(16777619)
	body = append(body, 0x6c, 0x21, 2)
	get(1)
	c(3)
	body = append(body, 0x6a, 0x21, 1)
	get(3)
	c(4)
	body = append(body, 0x6a, 0x21, 3)
	get(0)
	c(1)
	body = append(body, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x0b)
	// Every changed source cell is observable after the original scalar tail.
	for i := byte(0); i < 5; i++ {
		c(uint32(16 + i*4))
		get(i)
		body = append(body, 0x36, 0, 0)
	}
	c(0)
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func residualStoreOracle(initial []byte, count, seed, hash, pointer uint32, delta, offset [2]uint32, shift uint32) ([]byte, bool) {
	out := append([]byte{}, initial...)
	out[48] = 42
	var temp uint32
	for n := uint32(0); n < count; n++ {
		z := (seed ^ (seed >> 13)) * 0x85ebca6b
		values := [2]uint32{uint32(int32(z) >> (shift & 31)), uint32(int32(z+0x87654321) >> 3)}
		for i, v := range values {
			ea := uint64(pointer+delta[i]) + uint64(offset[i])
			if ea+4 > uint64(len(out)) {
				return out, true
			}
			binary.LittleEndian.PutUint32(out[int(ea):], v)
		}
		temp = z + 1
		hash = (hash ^ temp) * 16777619
		seed += 3
		pointer += 4
	}
	for i, v := range []uint32{0, seed, hash, pointer, temp} {
		binary.LittleEndian.PutUint32(out[16+i*4:], v)
	}
	return out, false
}

func TestIntegerResidualStoresMemoryTrapAndState(t *testing.T) {
	old := integerResidualStoresEnabled
	defer func() { integerResidualStoresEnabled = old }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*31 + 7)
	}
	type testCase struct {
		n, base       uint32
		delta, offset [2]uint32
		shift         uint32
	}
	var cases []testCase
	for _, n := range []uint32{0, 1, 4, 16, 17, 18, 19, 20, 21, 33, 65} {
		cases = append(cases, testCase{n: n, base: 128, delta: [2]uint32{0, 4096}, shift: 8})
	}
	for _, d := range []uint32{0, 1, 4, 16, 64, 67, 68, 69, 128} {
		cases = append(cases, testCase{n: 17, base: 128, delta: [2]uint32{0, d}, shift: 31})
	}
	cases = append(cases,
		testCase{n: 17, base: 128, delta: [2]uint32{4096, 0}, shift: 32},
		testCase{n: 17, base: 128, delta: [2]uint32{0, 4096}, offset: [2]uint32{3, 7}, shift: 63},
		testCase{n: 17, base: 65516, delta: [2]uint32{0, 0xfffff000}, shift: 8},
		testCase{n: 17, base: 128, delta: [2]uint32{0, 65536}, shift: 8},
		testCase{n: 17, base: 0xfffffffc, delta: [2]uint32{16, 4096}, shift: 8},
		testCase{n: 17, base: 128, delta: [2]uint32{0, 4096}, offset: [2]uint32{0xffffffff, 0}, shift: 8},
		testCase{n: 17, base: 65535, delta: [2]uint32{0, 4096}, shift: 8},
	)
	for _, on := range []bool{false, true} {
		integerResidualStoresEnabled = on
		for _, compact := range []bool{false, true} {
			for _, tc := range cases {
				m := residualStoreFixture(t, tc.delta, tc.offset, tc.shift)
				for _, seed := range []uint32{0x7ffffffd, 0xfffffff9} {
					want, traps := residualStoreOracle(initial, tc.n, seed, 0x811c9dc5, tc.base, tc.delta, tc.offset, tc.shift)
					got, err := runLinearSpanMemory(t, m, CompileOptions{CompactNative: compact}, initial, uint64(tc.n)|0xfeed00000000, uint64(seed)|0xdead00000000, 0x811c9dc5, uint64(tc.base)|0xbeef00000000)
					if (err != nil) != traps || !bytes.Equal(got, want) {
						first := -1
						for i := range want {
							if got[i] != want[i] {
								first = i
								break
							}
						}
						t.Fatalf("on%v compact%v case%+v seed%x err%v traps%v first%d", on, compact, tc, seed, err, traps, first)
					}
				}
			}
		}
	}
}

func TestIntegerResidualStoreAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := integerResidualStoresEnabled
	defer func() { integerResidualStoresEnabled = old }()
	for _, on := range []bool{false, true} {
		integerResidualStoresEnabled = on
		stats := &ModuleStats{}
		m := residualStoreFixture(t, [2]uint32{0, 4096}, [2]uint32{}, 8)
		cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if hit := stats.Funcs[0].Peephole["integer-residual-store-vector"] != 0; hit != on {
			t.Fatalf("on%v stats%v", on, stats.Funcs[0].Peephole)
		}
	}
}

func TestIntegerResidualStoreProbe(t *testing.T) {
	m := residualStoreFixture(t, [2]uint32{0, 4096}, [2]uint32{}, 8)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.ReaderFrom(m.Code[0].BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			t.Fatal(err)
		}
		if err = classifier.ClassifyInto(&r, op, &imm); err != nil {
			t.Fatal(err)
		}
		if op == 0x03 {
			break
		}
	}
	f := fn{m: m, nLocals: 5, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32}, classifier: classifier}
	start := r.Offset()
	prefix := f.hasIntegerResidualPrefix(&r)
	p, ok := f.inspectIntegerResidual(&r)
	if !prefix || !ok || r.Offset() != start || f.integerResidualWork > integerResidualWorkLimit {
		t.Fatalf("prefix%v accepted%v work%d nodes%d stores%d pointer%d seed%d hash%d count%d mapN%d", prefix, ok, p.work, p.n, p.storeN, p.pointer, p.seed, p.hash, p.counter, p.mapN)
	}
}

func TestIntegerResidualStoreProbeBudgetAndMemoryBarriers(t *testing.T) {
	old := integerResidualStoresEnabled
	integerResidualStoresEnabled = true
	defer func() { integerResidualStoresEnabled = old }()
	m := residualStoreFixture(t, [2]uint32{0, 4096}, [2]uint32{}, 8)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	r := wasm.ReaderFrom(m.Code[0].BodyBytes)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, _ := r.Byte()
		if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
			t.Fatal(err)
		}
		if op == 0x03 {
			break
		}
	}
	start := r.Offset()
	f := fn{m: m, nLocals: 5, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32}, classifier: classifier}
	if allocations := testing.AllocsPerRun(50, func() {
		f.integerResidualWork = 0
		_, ok := f.inspectIntegerResidual(&r)
		if !ok || r.Offset() != start {
			panic("invalid source probe")
		}
	}); allocations != 0 {
		t.Fatalf("probe allocated%v", allocations)
	}
	f.integerResidualWork = integerResidualWorkLimit - 1
	if _, ok := f.inspectIntegerResidual(&r); ok || f.integerResidualWork != integerResidualWorkLimit || r.Offset() != start {
		t.Fatal("budget or reader contract violated")
	}
	f.integerResidualWork = 0
	f.threadedMemory0 = true
	if _, ok := f.inspectIntegerResidual(&r); ok {
		t.Fatal("shared output memory admitted")
	}
	f.integerResidualWork = 0
	f.threadedMemory0 = false
	m.Memories[0].Limits.Addr64 = true
	if _, ok := f.inspectIntegerResidual(&r); ok {
		t.Fatal("memory64 output admitted")
	}
}
