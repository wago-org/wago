//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func residualTestBody(divisor, multiplier uint32) (body, loop []byte) {
	return residualTestBodySteps(divisor, multiplier, 1, false)
}
func residualTestBodySteps(divisor, multiplier uint32, steps int, early bool) (body, loop []byte) {
	c := func(v uint32) { loop = append(loop, 0x41); loop = append(loop, wasmtest.SLEB32(int32(v))...) }
	get := func(i byte) { loop = append(loop, 0x20, i) }
	advance := func() { get(1); c(3); loop = append(loop, 0x6a, 0x21, 1) }
	if early {
		advance()
	}
	get(1)
	get(1)
	c(13)
	loop = append(loop, 0x76, 0x73)
	c(0x85ebca6b)
	loop = append(loop, 0x6c)
	c(1023)
	loop = append(loop, 0x71, 0x21, 3)
	for i := 0; i < steps; i++ {
		get(3)
		c(0x87654321 + uint32(i)*99)
		loop = append(loop, 0x6c)
		c(0x40000000 + uint32(i)*31)
		loop = append(loop, 0x6a)
		c(divisor)
		loop = append(loop, 0x6d, 0x22, 4)
		get(2)
		loop = append(loop, 0x73)
		c(multiplier)
		loop = append(loop, 0x6c, 0x21, 2)
	}
	if !early {
		advance()
	}
	get(0)
	c(1)
	loop = append(loop, 0x6b, 0x22, 0, 0x0d, 0, 0x0b)
	body = []byte{1, 2, 0x7f, 0x02, 0x40, 0x20, 0, 0x0d, 0, 0x20, 2, 0x0f, 0x0b, 0x03, 0x40}
	body = append(body, loop...)
	body = append(body, 0x20, 2, 0x20, 3, 0x73, 0x20, 4, 0x73, 0x20, 1, 0x73, 0x20, 0, 0x73, 0x0b)
	return
}

func residualTestOracle(count, seed, hash, divisor, multiplier uint32) uint32 {
	if count == 0 {
		return hash
	}
	var coordinate, quotient uint32
	for n := uint32(0); n < count; n++ {
		coordinate = ((seed ^ (seed >> 13)) * 0x85ebca6b) & 1023
		numerator := coordinate*0x87654321 + 0x40000000
		quotient = uint32(int32(numerator) / int32(divisor))
		hash = (hash ^ quotient) * multiplier
		seed += 3
	}
	return hash ^ coordinate ^ quotient ^ seed
}
func TestIntegerResidualExactState(t *testing.T) {
	for _, divisor := range []uint32{1, 2, 3, 7, 10, 31, 1 << 30, 0x7fffffff} {
		for _, multiplier := range []uint32{3, 16777619, 0xfffffffb} {
			body, _ := residualTestBody(divisor, multiplier)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, count := range []uint32{0, 1, 4, 16, 17, 18, 19, 20, 21, 33, 65} {
				for _, seed := range []uint32{0, 0x7ffffffd, 0xfffffff9} {
					for _, enabled := range []bool{false, true} {
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						hash := uint32(0x811c9dc5)
						got, err := runArm64WrapperWithOptions(t, m, opts, uint64(count)|0xdeadbeef00000000, uint64(seed)|0xcafebabe00000000, uint64(hash))
						want := uint64(residualTestOracle(count, seed, hash, divisor, multiplier))
						if err != nil || got != want {
							t.Fatalf("d=%d c=%x n=%d seed=%x on=%v got=%x want=%x err=%v", divisor, multiplier, count, seed, enabled, got, want, err)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-residual-vector"] != 0) != enabled {
							t.Fatalf("residual not selected d=%d on=%v stats=%v", divisor, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
func TestIntegerResidualProbeBudgetAndEffects(t *testing.T) {
	_, valid := residualTestBody(10, 16777619)
	_, zero := residualTestBody(0, 16777619)
	_, negative := residualTestBody(0xffffffff, 16777619)
	for _, tc := range []struct {
		name string
		code []byte
		work uint16
		ok   bool
	}{
		{"valid", valid, 0, true}, {"zero-divisor", zero, 0, false}, {"negative-divisor", negative, 0, false},
		{"load", append([]byte{0x41, 0, 0x28, 0, 0}, valid...), 0, false},
		{"nested", append([]byte{0x02, 0x40}, valid...), 0, false},
		{"exhausted", valid, integerResidualWorkLimit, false}, {"last-unit", valid, integerResidualWorkLimit - 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{nLocals: 5, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32}, integerResidualWork: tc.work}
			r := wasm.ReaderFrom(tc.code)
			_, ok := f.inspectIntegerResidual(&r)
			if ok != tc.ok || r.Offset() != 0 || f.integerResidualWork > integerResidualWorkLimit {
				t.Fatalf("probe ok=%v offset=%d work=%d", ok, r.Offset(), f.integerResidualWork)
			}
			if allocs := testing.AllocsPerRun(25, func() { f.integerResidualWork = tc.work; f.inspectIntegerResidual(&r) }); allocs != 0 {
				t.Fatalf("probe allocated %v", allocs)
			}
		})
	}
}

func TestIntegerResidualOrderedMultiUpdate(t *testing.T) {
	for _, steps := range []int{2, 3, 4} {
		for _, early := range []bool{false, true} {
			body, _ := residualTestBodySteps(10, 16777619, steps, early)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, count := range []uint32{0, 1, 16, 17, 18, 19, 33, 65} {
				start := uint32(0xfffffffa)
				hash := uint32(0x811c9dc5)
				seed := start
				var coordinate, quotient uint32
				for n := uint32(0); n < count; n++ {
					if early {
						seed += 3
					}
					coordinate = ((seed ^ (seed >> 13)) * 0x85ebca6b) & 1023
					for i := 0; i < steps; i++ {
						num := coordinate*(0x87654321+uint32(i)*99) + 0x40000000 + uint32(i)*31
						quotient = uint32(int32(num) / 10)
						hash = (hash ^ quotient) * 16777619
					}
					if !early {
						seed += 3
					}
				}
				want := hash
				if count != 0 {
					want ^= coordinate ^ quotient ^ seed
				}
				for _, enabled := range []bool{false, true} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, uint64(count), uint64(start), 0x811c9dc5)
					if err != nil || got != uint64(want) {
						t.Fatalf("steps=%d early=%v n=%d on=%v got=%x want=%x err=%v", steps, early, count, enabled, got, want, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-residual-vector"] != 0) != enabled {
						t.Fatalf("multi-update not selected: %v", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}

func TestIntegerResidualPreservesCallerFloatingValue(t *testing.T) {
	body, _ := residualTestBodySteps(10, 16777619, 3, false)
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.F64}
	caller := []byte{1, 1, 0x7f, 0x20, 3, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0xbd, 0x20, 4, 0xad, 0x85, 0x0b}
	m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I64}, caller}, funcDef{params[:3], []wasm.ValType{wasm.I32}, body})
	for _, floating := range []uint64{0x401d000000000000, 0x8000000000000000, 0x7ff8000000000042} {
		for _, enabled := range []bool{false, true} {
			scalar, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": false}}, 33, 0xfffffff9, 0x811c9dc5, floating)
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, 33, 0xfffffff9, 0x811c9dc5, floating)
			if err != nil || got != scalar {
				t.Fatalf("caller FP=%x on=%v got=%x scalar=%x err=%v", floating, enabled, got, scalar, err)
			}
			if diagnosticsEnabled && (stats.Funcs[1].Peephole["integer-residual-vector"] != 0) != enabled {
				t.Fatalf("nested residual selection %v", stats.Funcs[1].Peephole)
			}
		}
	}
}

func TestIntegerResidualPressureRejectsBeforeEmission(t *testing.T) {
	_, code := residualTestBody(10, 16777619)
	probe := fn{nLocals: 5, localType: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32}}
	r := wasm.ReaderFrom(code)
	p, ok := probe.inspectIntegerResidual(&r)
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
		f.emitIntegerResidualPlan(&p)
		if len(f.a.B) != 0 || f.pinned != gp || f.fpinned != fp || f.integerResidualPlans != 0 {
			t.Fatalf("%s pressure rejection changed native state", kind)
		}
	}
}

func TestIntegerResidualVectorDivisionBoundaries(t *testing.T) {
	for _, d := range []uint32{3, 5, 10, 11, 13, 25, 0x7fffffff} {
		var loop []byte
		c := func(v uint32) { loop = append(loop, 0x41); loop = append(loop, wasmtest.SLEB32(int32(v))...) }
		loop = append(loop, 0x20, 1, 0x22, 3)
		c(d)
		loop = append(loop, 0x6d, 0x22, 4, 0x20, 2, 0x73)
		c(16777619)
		loop = append(loop, 0x6c, 0x21, 2, 0x20, 1)
		c(3)
		loop = append(loop, 0x6a, 0x21, 1, 0x20, 0)
		c(1)
		loop = append(loop, 0x6b, 0x22, 0, 0x0d, 0, 0x0b)
		body := []byte{1, 2, 0x7f, 0x02, 0x40, 0x20, 0, 0x0d, 0, 0x20, 2, 0x0f, 0x0b, 0x03, 0x40}
		body = append(body, loop...)
		body = append(body, 0x20, 2, 0x20, 3, 0x73, 0x20, 4, 0x73, 0x20, 1, 0x73, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, start := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0x80000001, 0xfffffff9, 0xffffffff, 7*d - 1} {
			for _, count := range []uint32{1, 16, 17, 18, 19, 33} {
				seed, hash := start, uint32(0x811c9dc5)
				var numerator, q uint32
				for n := uint32(0); n < count; n++ {
					numerator = seed
					q = uint32(int32(numerator) / int32(d))
					hash = (hash ^ q) * 16777619
					seed += 3
				}
				want := uint64(hash ^ numerator ^ q ^ seed)
				for _, enabled := range []bool{false, true} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"dot-loop-vector": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, uint64(count), uint64(start), 0x811c9dc5)
					if err != nil || got != want {
						t.Fatalf("direct d=%d seed=%x n=%d on=%v: got=%x want=%x err=%v", d, start, count, enabled, got, want, err)
					}
					if diagnosticsEnabled && d == 10 && (stats.Funcs[0].Peephole["integer-residual-vector-div"] != 0) != enabled {
						t.Fatalf("vector divide not selected: %v", stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
