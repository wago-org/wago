//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func maxSumFixture(t testing.TB, dst, a, b, stride, limit, bias uint32) *wasm.Module {
	body := []byte{1, 11, 0x7f}
	c := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	get := func(i byte) { body = append(body, 0x20, i) }
	c(0x8142f301)
	body = append(body, 0x21, 8, 0x03, 0x40)
	get(9)
	get(8)
	body = append(body, 0x36, 2, 0)
	get(8)
	c(1664525)
	body = append(body, 0x6c)
	c(1013904223)
	body = append(body, 0x6a, 0x21, 8)
	get(9)
	c(4)
	body = append(body, 0x6a, 0x22, 9)
	c(65536)
	body = append(body, 0x47, 0x0d, 0, 0x0b)
	for i, v := range []uint32{limit, dst, a, b, 0, 0x80000000, 0, 0, 0, 0, bias} {
		c(v)
		body = append(body, 0x21, byte(i))
	}
	c(64028)
	c(123)
	body = append(body, 0x03, 0x40)
	for lane := uint32(0); lane < 2; lane++ {
		get(1)
		get(5)
		get(2)
		if lane != 0 {
			c(stride)
			body = append(body, 0x6a)
		}
		body = append(body, 0x28, 2, 0)
		get(3)
		get(10)
		body = append(body, 0x6a)
		c(12 + lane*4)
		body = append(body, 0x6a, 0x22, 7, 0x28, 2, 0, 0x6a, 0x22, 6)
		get(5)
		get(6)
		body = append(body, 0x4a, 0x1b, 0x22, 5, 0x36, 2, 0)
	}
	for _, v := range []struct {
		local byte
		step  uint32
	}{{2, stride * 2}, {3, 8}, {4, 2}} {
		get(v.local)
		c(v.step)
		body = append(body, 0x6a, 0x21, v.local)
	}
	get(4)
	get(0)
	body = append(body, 0x47, 0x0d, 0, 0x0b, 0x36, 2, 0)
	for i, local := range []byte{2, 3, 4, 5, 6, 7} {
		c(64000 + uint32(i)*4)
		get(local)
		body = append(body, 0x36, 2, 0)
	}
	c(0)
	body = append(body, 0x0b)
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func maxSumOracle(dst, a, b, stride, limit, bias uint32) ([]byte, bool) {
	mem := make([]byte, 65536)
	value := uint32(0x8142f301)
	for off := 0; off < len(mem); off += 4 {
		binary.LittleEndian.PutUint32(mem[off:], value)
		value = value*1664525 + 1013904223
	}
	counter, acc, sum, lastB := uint32(0), uint32(0x80000000), uint32(0), uint32(0)
	for {
		for lane := uint32(0); lane < 2; lane++ {
			x, y := a+lane*stride, b+bias+12+lane*4
			if uint64(x)+4 > 65536 || uint64(y)+4 > 65536 {
				return mem, true
			}
			sum = binary.LittleEndian.Uint32(mem[x:]) + binary.LittleEndian.Uint32(mem[y:])
			lastB = y
			if int32(sum) > int32(acc) {
				acc = sum
			}
			if uint64(dst)+4 > 65536 {
				return mem, true
			}
			binary.LittleEndian.PutUint32(mem[dst:], acc)
		}
		a += stride * 2
		b += 8
		counter += 2
		if counter == limit {
			break
		}
	}
	binary.LittleEndian.PutUint32(mem[64028:], 123)
	for i, v := range []uint32{a, b, counter, acc, sum, lastB} {
		binary.LittleEndian.PutUint32(mem[64000+i*4:], v)
	}
	return mem, false
}

func TestMaxSumFullMemoryAndTraps(t *testing.T) {
	old := maxSumLoopEnabled
	defer func() { maxSumLoopEnabled = old }()
	for fixture := 0; fixture < 160; fixture++ {
		stride := []uint32{4, 8, 20, 720}[fixture%4]
		limit := []uint32{2, 4, 6, 14, 16, 18, 20, 30, 32, 34, 64}[fixture%11]
		a := []uint32{0, 1, 256, 4096, 65532, 65534, 0xfffffffe}[fixture%7]
		b := []uint32{0, 128, 8192, 65500, 65520, 65534, 0xfffffffc, 0x80001000}[fixture%8]
		bias := uint32(0)
		if fixture%8 == 7 {
			bias = 0x80000000
		}
		dst := []uint32{0, 1, 3, 64, 1000, 60000, 65532, 65534, 0xfffffffe, a + stride*3, b + bias + 15}[fixture%11]
		m := maxSumFixture(t, dst, a, b, stride, limit, bias)
		want, trap := maxSumOracle(dst, a, b, stride, limit, bias)
		for _, on := range []bool{false, true} {
			maxSumLoopEnabled = on
			for _, compact := range []bool{false, true} {
				got, err := runScalarUnrollMemory(t, m, CompileOptions{CompactNative: compact})
				var tr *rt.TrapError
				if (err != nil) != trap || err != nil && (!errors.As(err, &tr) || tr.Code != rt.TrapLinMemOutOfBounds) || !bytes.Equal(got, want) {
					t.Fatalf("case%d dst%x a%x b%x stride%d limit%d bias%x on%v compact%v trap%v err%v memoryEqual%v", fixture, dst, a, b, stride, limit, bias, on, compact, trap, err, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestMaxSumAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, stride := range []uint32{4, 20, 720} {
		stats := compileWithStats(t, maxSumFixture(t, 60000, 256, 8192, stride, 32, 0), false).Funcs[0]
		if stats.Peephole["max-sum-vector"] != 1 {
			t.Fatalf("stride%d not admitted: %v", stride, stats.Peephole)
		}
	}
}

func TestMaxSumProbeBudgetEffectsAndAllocations(t *testing.T) {
	m := maxSumFixture(t, 60000, 256, 8192, 720, 32, 0)
	body := m.Code[0].BodyBytes
	at := bytes.LastIndex(body, []byte{0x03, 0x40})
	if at < 0 {
		t.Fatal("missing fixture loop")
	}
	code := body[at+2:]
	probe := fn{nLocals: 11, localType: make([]machineType, 11)}
	for i := range probe.localType {
		probe.localType[i] = mtI32
	}
	var admitted bool
	allocs := testing.AllocsPerRun(100, func() {
		probe.integerResidualWork = 0
		r := wasm.ReaderFrom(code)
		_, admitted = probe.inspectMaxSumLoop(&r)
		if r.Offset() != 0 {
			panic("candidate advanced source reader")
		}
	})
	if !admitted || allocs != 0 {
		t.Fatalf("admitted%v allocations%g", admitted, allocs)
	}
	for _, prefix := range [][]byte{
		{0x41, 0, 0x28, 2, 0, 0x21, 6}, // an unused, potentially trapping fifth load
		{0x41, 0, 0x41, 0, 0x36, 2, 0}, // an additional ordered store
		{0x10, 0}, {0x02, 0x40}, {0x40, 0}, {0x00},
	} {
		probe.integerResidualWork = 0
		r := wasm.ReaderFrom(append(append([]byte{}, prefix...), code...))
		if _, ok := probe.inspectMaxSumLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("unsafe prefix%x admitted%v offset%d", prefix, ok, r.Offset())
		}
	}
	probe.integerResidualWork = integerResidualWorkLimit - 3
	r := wasm.ReaderFrom(code)
	if _, ok := probe.inspectMaxSumLoop(&r); ok || probe.integerResidualWork != integerResidualWorkLimit || r.Offset() != 0 {
		t.Fatal("budget exhaustion did not reject transactionally")
	}
	r = wasm.ReaderFrom(code)
	if _, ok := probe.inspectMaxSumLoop(&r); ok || probe.integerResidualWork != integerResidualWorkLimit {
		t.Fatal("exhausted budget admitted another probe")
	}
}

func TestMaxSumPressureRejectsBeforeEmission(t *testing.T) {
	m := maxSumFixture(t, 60000, 256, 8192, 720, 32, 0)
	body := m.Code[0].BodyBytes
	at := bytes.LastIndex(body, []byte{0x03, 0x40})
	probe := fn{nLocals: 11, localType: make([]machineType, 11)}
	for i := range probe.localType {
		probe.localType[i] = mtI32
	}
	r := wasm.ReaderFrom(body[at+2:])
	p, ok := probe.inspectMaxSumLoop(&r)
	if !ok {
		t.Fatal("fixture rejected")
	}
	for _, kind := range []string{"gp", "fp"} {
		f := gpClassEmitter()
		if kind == "gp" {
			f.pinned = allAllocatableGP()
		} else {
			for _, reg := range fpAllocRegs {
				f.fpinned = f.fpinned.add(reg)
			}
		}
		gp, fp := f.pinned, f.fpinned
		f.emitMaxSumPlan(&p)
		if len(f.a.B) != 0 || f.pinned != gp || f.fpinned != fp {
			t.Fatalf("%s pressure rejection mutated native state", kind)
		}
	}
}

func TestMaxSumCallerFPAndVectors(t *testing.T) {
	callee := maxSumFixture(t, 60000, 256, 8192, 720, 64, 0)
	args := [4]uint64{0, 0, 0, 0}
	want, trapped := maxSumOracle(60000, 256, 8192, 720, 64, 0)
	if trapped {
		t.Fatal("invalid caller fixture")
	}
	b := []byte{2, 12, 0x7c, 4, 0x7b}
	for i := byte(0); i < 12; i++ {
		value := uint64(0x7ff0000012345678) + uint64(i)*0x100000001
		b = append(b, 0x44)
		b = binary.LittleEndian.AppendUint64(b, value)
		b = append(b, 0x21, i+4)
		binary.LittleEndian.PutUint64(want[32768+int(i)*8:], value)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0xfd, 0x0c)
		for lane := byte(0); lane < 16; lane++ {
			b = append(b, i*32+lane+1)
			want[33024+int(i)*16+int(lane)] = i*32 + lane + 1
		}
		b = append(b, 0x21, i+16)
	}
	b = append(b, 0x41)
	b = append(b, wasmtest.SLEB32(33280)...)
	b = append(b, 0x20, 16)

	b = append(b, 0x10, 1, 0x1a, 0xfd, 0x0b, 0, 0)
	copy(want[33280:33296], want[33024:33040])
	for i := byte(0); i < 12; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(32768+int32(i)*8)...)
		b = append(b, 0x20, i+4, 0x39, 0, 0)
	}
	for i := byte(0); i < 4; i++ {
		b = append(b, 0x41)
		b = append(b, wasmtest.SLEB32(33024+int32(i)*16)...)
		b = append(b, 0x20, i+16, 0xfd, 0x0b, 0, 0)
	}
	b = append(b, 0x41, 0, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
	m.Types = append(m.Types, callee.Types[0])
	m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: 1})
	m.Code = append(m.Code, callee.Code[0])
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		for _, stackReg := range []bool{false, true} {
			stats := &ModuleStats{}
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false, "stack-reg": stackReg}}
			if diagnosticsEnabled {
				opts.Stats = stats
			}
			got, err := runScalarUnrollMemory(t, m, opts, args[:]...)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("compact%v stackReg%v err%v equal%v", compact, stackReg, err, bytes.Equal(got, want))
			}
			if diagnosticsEnabled && stats.Funcs[1].Peephole["max-sum-vector"] != 1 {
				t.Fatalf("callee did not vectorize: %v", stats.Funcs[1].Peephole)
			}
		}
	}
}
