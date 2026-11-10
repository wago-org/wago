//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/big"
	"testing"
	"unsafe"
)

func affineLoopFixture(t testing.TB, global bool, family int, start, seed uint32, trap bool) *wasm.Module {
	constant := func(x uint32) []byte { return append([]byte{0x41}, wasmtest.SLEB32(int32(x))...) }
	localN := byte(3)
	if global {
		localN = 2
	}
	if family == 2 && !global {
		localN = 5
	}
	b := []byte{1, localN, 0x7f}
	b = append(b, constant(start)...)
	b = append(b, 0x21, 1)
	b = append(b, constant(seed)...)
	b = append(b, 0x21, 2)
	get := func(x byte) []byte {
		if global {
			return []byte{0x23, x}
		}
		return []byte{0x20, 3 + x}
	}
	set := func(x byte) []byte {
		if global {
			return []byte{0x24, x}
		}
		return []byte{0x21, 3 + x}
	}
	if !global {
		for i := byte(0); i < 1+2*byte(family/2); i++ {
			b = append(b, constant(0x80000007+uint32(i))...)
			b = append(b, set(i)...)
		}
	}
	b = append(b, 0x03, 0x40, 0x20, 2, 0x02, 0x7f)
	switch family {
	case 0:
		b = append(b, get(0)...)
		b = append(b, 0x20, 1, 0x6a)
		b = append(b, set(0)...)
		b = append(b, get(0)...)
	case 1:
		b = append(b, get(0)...)
	case 2:
		b = append(b, 0x20, 1)
		b = append(b, set(0)...)
		b = append(b, 0x20, 1, 0x41, 3, 0x6a)
		b = append(b, set(1)...)
		b = append(b, get(0)...)
		b = append(b, set(2)...)
		b = append(b, get(1)...)
		b = append(b, set(0)...)
		b = append(b, get(2)...)
		b = append(b, set(1)...)
		b = append(b, get(0)...)
		b = append(b, get(1)...)
		b = append(b, 0x6b)
	}
	b = append(b, 0x0b, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b)
	if trap {
		b = append(b, 0x00)
	}
	// Keep the counter and accumulator observable, in addition to every global.
	b = append(b, 0x20, 2, 0xad, 0x20, 1, 0xad, 0x42, 32, 0x86, 0x84, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))), wasmtest.Section(3, wasmtest.Vec([]byte{0}))}
	if global {
		gs := [][]byte{{0x7f, 1, 0x41, 0, 0x0b}}
		if family == 2 {
			gs = append(gs, []byte{0x7f, 1, 0x41, 0, 0x0b}, []byte{0x7f, 1, 0x41, 0, 0x0b})
		}
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(gs...)))
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("state", 3, 0))))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func runAffineLoopCells(t *testing.T, m *wasm.Module, opts CompileOptions, arg uint64, values ...uint64) (uint64, []uint64, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	ar, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := rt.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(code)
	cells := make([][]byte, len(values))
	table := ar.Alloc(8 * len(values))
	for i, v := range values {
		cells[i] = ar.Alloc(8)
		binary.LittleEndian.PutUint64(cells[i], v)
		binary.LittleEndian.PutUint64(table[i*8:], uint64(uintptr(unsafe.Pointer(&cells[i][0]))))
	}
	if len(table) != 0 {
		jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&table[0])))
	}
	args, result, trap := ar.Alloc(16), ar.Alloc(16), ar.Alloc(rt.TrapBufferBytes)
	binary.LittleEndian.PutUint64(args, arg)
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, result)
	after := make([]uint64, len(cells))
	for i, c := range cells {
		after[i] = binary.LittleEndian.Uint64(c)
	}
	return binary.LittleEndian.Uint64(result), after, err
}

func TestAffineLoopSummaryLocalAndGlobalSemantics(t *testing.T) {
	old := affineLoopSummaryEnabled
	t.Cleanup(func() { affineLoopSummaryEnabled = old })
	for _, global := range []bool{false, true} {
		for family := 0; family < 3; family++ {
			for _, start := range []uint32{0, 7, ^uint32(0) - 1, ^uint32(0)} {
				for _, trap := range []bool{false, true} {
					seed := uint32(0xfffffff7)
					m := affineLoopFixture(t, global, family, start, seed, trap)
					for _, on := range []bool{false, true} {
						affineLoopSummaryEnabled = on
						for _, compact := range []bool{false, true} {
							for _, limit := range []uint64{0, 1, 2, 15, 16, 17, 31, 257, 1<<32 | 31} {
								gs := []uint64(nil)
								if global {
									gs = []uint64{0x80000007}
									if family == 2 {
										gs = append(gs, 0x80000008, 0x80000009)
									}
								}
								i, a, g := start, seed, uint32(0x80000007)
								last := i
								for {
									last = i
									switch family {
									case 0:
										g += i
										a += g
									case 1:
										a += g
									case 2:
										a += 3
									}
									i++
									if i >= uint32(limit) {
										break
									}
								}
								got, cells, err := runAffineLoopCells(t, m, CompileOptions{CompactNative: compact}, limit, gs...)
								want := uint64(i)<<32 | uint64(a)
								if (err != nil) != trap || !trap && got != want {
									t.Fatalf("global%v family%d start%x trap%v on%v compact%v limit%d got%x want%x err%v", global, family, start, trap, on, compact, limit, got, want, err)
								}
								if global {
									switch family {
									case 0:
										if uint32(cells[0]) != g {
											t.Fatalf("global prefix state%x want%x", cells[0], g)
										}
									case 1:
										if uint32(cells[0]) != 0x80000007 {
											t.Fatal("changed read-only global")
										}
									case 2:
										if uint32(cells[0]) != last+3 || uint32(cells[1]) != last || uint32(cells[2]) != last {
											t.Fatalf("last-iteration globals%x last%x", cells, last)
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestAffineLoopSummaryAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	if profileEnabled {
		t.Skip("ordinary emitter qualification")
	}
	old := affineLoopSummaryEnabled
	t.Cleanup(func() { affineLoopSummaryEnabled = old })
	for _, global := range []bool{false, true} {
		for family := 0; family < 3; family++ {
			for _, on := range []bool{false, true} {
				affineLoopSummaryEnabled = on
				var stats ModuleStats
				c, err := CompileModuleWith(affineLoopFixture(t, global, family, 0, 0, false), CompileOptions{Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				c.CodeImage.Close()
				if hit := stats.Funcs[0].Peephole["affine-loop-summary"]; (hit != 0) != on {
					t.Fatalf("global%v family%d on%v hit%d other%v", global, family, on, hit, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestAffineLoopSummaryFullCounterWrap(t *testing.T) {
	requireCompilerDiagnostics(t)
	if profileEnabled {
		t.Skip("ordinary emitter qualification")
	}
	old := affineLoopSummaryEnabled
	affineLoopSummaryEnabled = true
	t.Cleanup(func() { affineLoopSummaryEnabled = old })
	for family := 0; family < 3; family++ {
		for _, start := range []uint32{0, ^uint32(0)} {
			m := affineLoopFixture(t, true, family, start, 0x80000001, false)
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			c.CodeImage.Close()
			if stats.Funcs[0].Peephole["affine-loop-summary"] != 1 {
				t.Fatal("huge-trip execution requires qualified fast path")
			}
			limit := uint32(^uint32(0))
			first := start + 1
			n := uint64(1)
			if first < limit {
				n = uint64(limit) - uint64(first) + 1
			}
			N := new(big.Int).SetUint64(n)
			I := new(big.Int).SetUint64(uint64(start))
			G := new(big.Int).SetUint64(0x80000007)
			A := new(big.Int).SetUint64(0x80000001)
			tri := new(big.Int).Mul(N, new(big.Int).Sub(N, big.NewInt(1)))
			tri.Div(tri, big.NewInt(2))
			g := new(big.Int).Add(G, new(big.Int).Mul(N, I))
			g.Add(g, tri)
			a := new(big.Int).Add(A, new(big.Int).Mul(N, G))
			u := new(big.Int).Mul(N, new(big.Int).Add(N, big.NewInt(1)))
			u.Div(u, big.NewInt(2))
			a.Add(a, new(big.Int).Mul(u, I))
			u.Mul(N, new(big.Int).Sub(N, big.NewInt(1)))
			u.Mul(u, new(big.Int).Add(N, big.NewInt(1)))
			u.Div(u, big.NewInt(6))
			a.Add(a, u)
			if family == 1 {
				a.Add(A, new(big.Int).Mul(N, G))
				g = G
			}
			if family == 2 {
				a.Add(A, new(big.Int).Mul(N, big.NewInt(3)))
			}
			globals := []uint64{0x80000007}
			if family == 2 {
				globals = append(globals, 0x80000008, 0x80000009)
			}
			got, cells, err := runAffineLoopCells(t, m, CompileOptions{}, uint64(limit), globals...)
			want := uint64(uint32(uint64(start)+n))<<32 | uint64(uint32(a.Uint64()))
			if err != nil || got != want {
				t.Fatalf("family%d start%x n%d got%x want%x err%v", family, start, n, got, want, err)
			}
			if family != 2 && uint32(cells[0]) != uint32(g.Uint64()) {
				t.Fatalf("large global%x want%x", cells[0], g.Uint64())
			}
			if family == 2 {
				last := uint32(uint64(start) + n - 1)
				if uint32(cells[0]) != last+3 || uint32(cells[1]) != last || uint32(cells[2]) != last {
					t.Fatalf("large last-iteration globals%x last%x", cells, last)
				}
			}
		}
	}
}

func affineLoopProbe(t testing.TB, global bool) (*fn, []byte) {
	m := affineLoopFixture(t, global, 0, 0, 0, false)
	types := []machineType{mtI32, mtI32, mtI32}
	if !global {
		types = append(types, mtI32)
	}
	f := &fn{m: m, nLocals: len(types), localType: types}
	body := m.Code[0].BodyBytes
	at := bytes.Index(body, []byte{0x03, 0x40})
	if at < 0 {
		t.Fatal("loop missing")
	}
	return f, body[at+2:]
}

func TestAffineLoopSummaryProbeBudgetAndRejections(t *testing.T) {
	f, body := affineLoopProbe(t, false)
	if allocs := testing.AllocsPerRun(100, func() {
		f.affineLoopWork = 0
		r := wasm.ReaderFrom(body)
		p, ok := f.inspectAffineLoop(&r)
		if !ok || p.n != 4 || r.Offset() != 0 {
			panic("invalid affine probe")
		}
	}); allocs != 0 {
		t.Fatalf("probe allocations%g", allocs)
	}
	for _, prefix := range [][]byte{
		{0x20, 1, 0x28, 2, 0, 0x1a}, {0x41, 1, 0x41, 0, 0x6e, 0x1a}, {0x10, 0}, {0x0d, 1}, {0x03, 0x40}, {0x04, 0x40}, {0x00},
		{0x20, 1, 0x20, 2, 0x6c, 0x1a}, bytes.Repeat([]byte{0x01}, 129),
	} {
		f.affineLoopWork = 0
		r := wasm.ReaderFrom(append(append([]byte{}, prefix...), body...))
		if _, ok := f.inspectAffineLoop(&r); ok || r.Offset() != 0 {
			t.Fatalf("admitted unsupported prefix%x", prefix)
		}
	}
	f.affineLoopWork = affineLoopWorkLimit - 1
	r := wasm.ReaderFrom(body)
	if _, ok := f.inspectAffineLoop(&r); ok || f.affineLoopWork != affineLoopWorkLimit || r.Offset() != 0 {
		t.Fatal("probe work cap or reader contract")
	}
	// A valid affine transfer need not be nilpotent. Multiplying a carried value
	// by three must not be mistaken for the finite binomial recipe.
	f.affineLoopWork = 0
	nonlinear := []byte{0x20, 3, 0x41, 3, 0x6c, 0x21, 3, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b}
	r = wasm.ReaderFrom(nonlinear)
	if _, ok := f.inspectAffineLoop(&r); ok {
		t.Fatal("accepted non-nilpotent transfer")
	}
}

func TestAffineLoopSummaryRejectsImportedGlobalCells(t *testing.T) {
	b := []byte{1, 2, 0x7f, 0x03, 0x40, 0x20, 2, 0x23, 0, 0x20, 1, 0x6a, 0x24, 0, 0x23, 0, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b}
	imp := append(wasmtest.Name("env"), wasmtest.Name("state")...)
	imp = append(imp, 3, 0x7f, 1)
	raw := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(b))), b...))))
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f := fn{m: m, nLocals: 3, localType: []machineType{mtI32, mtI32, mtI32}}
	r := wasm.ReaderFrom(m.Code[0].BodyBytes[2:])
	if _, ok := f.inspectAffineLoop(&r); ok {
		t.Fatal("admitted imported global alias")
	}
}

func TestAffineLoopSummarySiblingWorkspaces(t *testing.T) {
	old := affineLoopSummaryEnabled
	affineLoopSummaryEnabled = true
	t.Cleanup(func() { affineLoopSummaryEnabled = old })
	m := affineLoopFixture(t, true, 0, 0, 9, false)
	f, body := affineLoopProbe(t, true)
	r := wasm.ReaderFrom(body)
	p, ok := f.inspectAffineLoop(&r)
	if !ok {
		t.Fatal("probe missing")
	}
	original := m.Code[0].BodyBytes
	start := bytes.Index(original, []byte{0x03, 0x40})
	end := start + 2 + p.end
	loop := append([]byte{}, original[start:end]...)
	next := append([]byte{}, original[:end]...)
	next = append(next, 0x41, 0, 0x21, 1)
	next = append(next, loop...)
	next = append(next, original[end:]...)
	m.Code[0].BodyBytes = next
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []uint64{1, 16, 17, 257} {
		g, a := uint32(3), uint32(9)
		for pass := 0; pass < 2; pass++ {
			for i := uint32(0); ; {
				g += i
				a += g
				i++
				if i >= uint32(limit) {
					break
				}
			}
		}
		got, cells, err := runAffineLoopCells(t, m, CompileOptions{}, limit, 3)
		want := uint64(uint32(limit))<<32 | uint64(a)
		if err != nil || got != want || uint32(cells[0]) != g {
			t.Fatalf("sibling limit%d got%x want%x globals%x wantG%x err%v", limit, got, want, cells, g, err)
		}
	}
}

func TestAffineLoopSummaryUnpinnedOutputJoin(t *testing.T) {
	old := affineLoopSummaryEnabled
	t.Cleanup(func() { affineLoopSummaryEnabled = old })
	base := affineLoopFixture(t, false, 0, 0, 0xfffffff7, false)
	// Twelve hotter anchors consume the whole-function pin pool. The summarized
	// loop's outputs therefore use frame homes with argument-register pins
	// disabled. A subsequent read must work on
	// both the scalar and fast edges, without forwarding a fast-only store.
	body := []byte{1, 15, 0x7f}
	for local := byte(4); local <= 15; local++ {
		for range 64 {
			body = append(body, 0x20, local, 0x41, 1, 0x6a, 0x21, local)
		}
	}
	source := base.Code[0].BodyBytes
	// Read the last published cell (the counter) immediately at the join.
	body = append(body, source[:len(source)-11]...)
	body = append(body, 0x20, 1, 0xad, 0x42, 32, 0x86, 0x20, 2, 0xad, 0x84, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, body)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		affineLoopSummaryEnabled = on
		for _, compact := range []bool{false, true} {
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"entry-arg-pins": false}}
			if diagnosticsEnabled && !profileEnabled {
				var stats ModuleStats
				opts.Stats = &stats
				c, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				c.CodeImage.Close()
				if on && stats.Funcs[0].Peephole["affine-loop-unpinned-output"] == 0 {
					t.Fatalf("pressure fixture did not exercise frame output publication: %v", stats.Funcs[0].Peephole)
				}
				opts.Stats = nil
			}
			for _, limit := range []uint64{1, 17, 64, 257} {
				g, a := uint32(0x80000007), uint32(0xfffffff7)
				for i := uint32(0); i < uint32(limit); i++ {
					g += i
					a += g
				}
				got, _, err := runAffineLoopCells(t, m, opts, limit)
				want := limit<<32 | uint64(a)
				if err != nil || got != want {
					t.Fatalf("unpinned on%v compact%v limit%d got%x want%x err%v", on, compact, limit, got, want, err)
				}
			}
		}
	}
}
