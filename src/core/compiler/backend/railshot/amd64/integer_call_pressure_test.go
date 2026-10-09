//go:build linux && amd64 && !tinygo

package amd64

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	cr "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type integerPressureCase struct {
	width, live, seed int
	call              bool
}

func (p integerPressureCase) raw() []byte {
	typ, constant, xor, add, div, shift, store, load, align := i32, byte(0x41), byte(0x73), byte(0x6a), byte(0x6d), byte(0x74), byte(0x36), byte(0x28), byte(2)
	if p.width == 64 {
		typ, constant, xor, add, div, shift, store, load, align = i64, 0x42, 0x85, 0x7c, 0x7f, 0x86, 0x37, 0x29, 3
	}
	results := make([]wasm.ValType, p.live+1)
	for i := range results {
		results[i] = typ
	}
	// Callee increments a separate work counter and computes from memory written
	// by this invocation. It takes no arguments, avoiding argument permutation work.
	callee := []byte{0x41, 16, 0x41, 16, 0x28, 2, 0, 0x41, 1, 0x6a, 0x36, 2, 0,
		0x41, 0, load, align, 0, constant, 0x35, xor}
	body := []byte{0, 0x41, 0, 0x20, 0, store, align, 0}
	for i := 0; i < p.live; i++ {
		salt := byte(1 + (i+p.seed*7)%31)
		body = append(body, 0x20, 0, constant, salt, xor, 0x20, 2, div+byte((i+p.seed)%4),
			0x20, 1, shift+byte((i+p.seed)%5), constant, byte(i+1), add)
	}
	if p.call {
		body = append(body, 0x10, 1)
	} else {
		body = append(body, callee...)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ}, results), wasmtest.FuncType(nil, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code(append(slices.Clone(callee), 0x0b)))),
	)
}

func (p integerPressureCase) expected(in [3]uint64) []uint64 {
	out := make([]uint64, p.live+1)
	for i := 0; i < p.live; i++ {
		x := in[0] ^ uint64(1+(i+p.seed*7)%31)
		x = divideResult(p.width, (i+p.seed)%4, x, in[2])
		out[i] = shiftResult(p.width, (i+p.seed)%5, x, in[1]) + uint64(i+1)
	}
	out[p.live] = in[0] ^ 0x35
	if p.width == 32 {
		for i := range out {
			out[i] = uint64(uint32(out[i]))
		}
	}
	return out
}

func integerPressureInputs() [][3]uint64 {
	inputs := make([][3]uint64, 0, 52)
	for _, x := range []uint64{0, 1, ^uint64(0), 1 << 31, 1 << 63, 0x8123456789abcdef} {
		for _, count := range shiftBoundaryCounts {
			divisor := uint64(3)
			if count&1 != 0 {
				divisor = ^uint64(2)
			} // -3; never zero or signed overflow divisor -1
			inputs = append(inputs, [3]uint64{x, count, divisor})
		}
	}
	state := uint64(810)
	for i := 0; i < 16; i++ {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		inputs = append(inputs, [3]uint64{state, state >> 32, 3})
	}
	return inputs
}

func compileIntegerPressure(tb testing.TB, raw []byte, stats *ModuleStats) *enc.CompiledModule {
	tb.Helper()
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		tb.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: 0,
		Optimizations: map[string]bool{"inline": false}, Stats: stats})
	if err != nil {
		tb.Fatal(err)
	}
	return cm
}

// Totals qualify a function with pressure and a real call, not the location of
// individual spills. They are not independent #807 transport proofs.
func integerPressureEvidence(stats *ModuleStats, high bool) error {
	if stats == nil || len(stats.Funcs) < 2 {
		return fmt.Errorf("diagnostic coverage unavailable")
	}
	f := stats.Funcs[0]
	calls := 0
	for kind, n := range f.Calls {
		if kind == callKindInline {
			return fmt.Errorf("inline call evidence")
		}
		calls += n
	}
	if calls != 1 {
		return fmt.Errorf("real call count %d, want 1", calls)
	}
	if high && (f.Spills == 0 || f.Reloads == 0) {
		return fmt.Errorf("pressure evidence: spills=%d reloads=%d", f.Spills, f.Reloads)
	}
	return nil
}

type integerPressureExecutor struct {
	engine                       *cr.Engine
	memory                       *cr.JobMemory
	entry                        uintptr
	args, results, guarded, trap []byte
	base                         uintptr
}

func prepareIntegerPressure(tb testing.TB, cm *enc.CompiledModule, live int) *integerPressureExecutor {
	tb.Helper()
	e := new(integerPressureExecutor)
	var err error
	e.engine, err = cr.NewEngine()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { e.engine.Close() })
	e.memory, err = cr.NewJobMemory(65536)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { e.memory.Close() })
	ar, err := cr.NewArena(4096)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { ar.Close() })
	code, base, err := cr.MapCode(cm.Code)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { cr.Unmap(code) })
	if !bytes.Equal(code[:len(cm.Code)], cm.Code) {
		tb.Fatal("mapped native bytes differ")
	}
	e.entry = base + uintptr(cm.Entry[0])
	e.args = ar.Alloc(24)
	e.guarded = ar.Alloc((live+1)*8 + 32)
	e.results = e.guarded[16 : len(e.guarded)-16]
	e.trap = ar.Alloc(cr.TrapBufferBytes)
	if err = e.memory.BindTrapCell(e.trap); err != nil {
		tb.Fatal(err)
	}
	e.base = e.memory.LinMemBase()
	return e
}
func (e *integerPressureExecutor) input(in [3]uint64) {
	for i, x := range in {
		binary.LittleEndian.PutUint64(e.args[i*8:], x)
	}
	for i := range e.guarded {
		e.guarded[i] = 0xa5
	}
	clear(e.memory.LinearMemory()[:32])
}
func (e *integerPressureExecutor) call() error {
	return e.engine.CallPrepared(e.entry, e.args, e.base, e.trap, e.results)
}
func (e *integerPressureExecutor) check(p integerPressureCase, in [3]uint64, want []uint64, calls uint32) error {
	for i, x := range want {
		got := binary.LittleEndian.Uint64(e.results[i*8:])
		if p.width == 32 {
			got = uint64(uint32(got))
		}
		if got != x {
			return fmt.Errorf("slot %d = %#x, want %#x", i, got, x)
		}
	}
	for i, x := range in {
		if binary.LittleEndian.Uint64(e.args[i*8:]) != x {
			return fmt.Errorf("input %d changed", i)
		}
	}
	for _, guard := range [][]byte{e.guarded[:16], e.guarded[len(e.guarded)-16:]} {
		for _, x := range guard {
			if x != 0xa5 {
				return fmt.Errorf("result guard changed")
			}
		}
	}
	if got := binary.LittleEndian.Uint32(e.memory.LinearMemory()[16:]); got != calls {
		return fmt.Errorf("work count %d, want %d", got, calls)
	}
	return nil
}

func TestIntegerLiveAcrossCallPressure(t *testing.T) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 6, 8, 12, 24} {
			for seed := 0; seed < 3; seed++ {
				p := integerPressureCase{width, live, seed, true}
				t.Run(fmt.Sprintf("i%d/live=%d/seed=%d", width, live, seed), func(t *testing.T) {
					raw := p.raw()
					var stats *ModuleStats
					if diagnosticsEnabled {
						stats = new(ModuleStats)
					}
					cm := compileIntegerPressure(t, raw, stats)
					if stats != nil {
						if err := integerPressureEvidence(stats, live >= 12); err != nil {
							t.Fatal(err)
						}
						f := stats.Funcs[0]
						t.Logf("path-shared=%t/%t code=%d frame=%d spills=%d reloads=%d calls=%v", f.SharedScalar, stats.Funcs[1].SharedScalar, f.CodeBytes, f.FrameBytes, f.Spills, f.Reloads, f.Calls)
					} else {
						t.Log("call/pressure/path evidence unavailable in ordinary build; no transport-proof claim")
					}
					t.Logf("source=%x native=%x selected-cpu=0 required-cpu=%d bounds=explicit api=Engine.CallPrepared", sha256.Sum256(raw), sha256.Sum256(cm.Code), cm.RequiredAMD64Features)
					e := prepareIntegerPressure(t, cm, live)
					for _, in := range integerPressureInputs() {
						want := p.expected(in)
						e.input(in)
						if err := e.call(); err != nil {
							t.Fatal(err)
						}
						if err := e.check(p, in, want, 1); err != nil {
							t.Fatalf("input=%x: %v", in, err)
						}
					}
				})
			}
		}
	}
}

func TestIntegerPressureObserverControls(t *testing.T) {
	p := integerPressureCase{64, 24, 1, true}
	cm := compileIntegerPressure(t, p.raw(), nil)
	e := prepareIntegerPressure(t, cm, p.live)
	in := [3]uint64{0x8123456789abcdef, 7, 3}
	want := p.expected(in)
	e.input(in)
	if err := e.call(); err != nil {
		t.Fatal(err)
	}
	if err := e.check(p, in, want, 1); err != nil {
		t.Fatal(err)
	}
	for i := range want {
		e.results[i*8] ^= 1
		if err := e.check(p, in, want, 1); err == nil {
			t.Fatalf("slot %d corruption accepted", i)
		}
		e.results[i*8] ^= 1
	}
	if want[0] == want[1] {
		t.Fatal("swap control needs distinct results")
	}
	a, b := binary.LittleEndian.Uint64(e.results), binary.LittleEndian.Uint64(e.results[8:])
	binary.LittleEndian.PutUint64(e.results, b)
	binary.LittleEndian.PutUint64(e.results[8:], a)
	if err := e.check(p, in, want, 1); err == nil {
		t.Fatal("slot permutation accepted")
	}
	binary.LittleEndian.PutUint64(e.results, a)
	binary.LittleEndian.PutUint64(e.results[8:], b)
	for _, offset := range []int{0, len(e.guarded) - 1} {
		e.guarded[offset] ^= 1
		if err := e.check(p, in, want, 1); err == nil || !strings.Contains(err.Error(), "guard") {
			t.Fatalf("guard control: %v", err)
		}
		e.guarded[offset] ^= 1
	}
	e.args[0] ^= 1
	if err := e.check(p, in, want, 1); err == nil || !strings.Contains(err.Error(), "input") {
		t.Fatalf("input control: %v", err)
	}
	e.args[0] ^= 1
	if err := e.check(p, in, want, 0); err == nil {
		t.Fatal("missing-work expectation accepted")
	}
}

func TestIntegerPressureNoCallControl(t *testing.T) {
	requireCompilerDiagnostics(t)
	p := integerPressureCase{64, 24, 1, false}
	stats := new(ModuleStats)
	cm := compileIntegerPressure(t, p.raw(), stats)
	e := prepareIntegerPressure(t, cm, p.live)
	in := [3]uint64{0x8123456789abcdef, 7, 3}
	e.input(in)
	if err := e.call(); err != nil {
		t.Fatal(err)
	}
	if err := e.check(p, in, p.expected(in), 1); err != nil {
		t.Fatal(err)
	}
	if err := integerPressureEvidence(stats, true); err == nil || !strings.Contains(err.Error(), "real call count 0") {
		t.Fatalf("no-call rejection: %v", err)
	}
}

func BenchmarkIntegerLiveAcrossCall(b *testing.B) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 24} {
			p := integerPressureCase{width, live, 1, true}
			raw := p.raw()
			name := fmt.Sprintf("i%d/live=%d", width, live)
			b.Run("compile/"+name, func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				var cm *enc.CompiledModule
				for i := 0; i < b.N; i++ {
					cm = compileIntegerPressure(b, raw, nil)
				}
				b.StopTimer()
				b.ReportMetric(float64(len(cm.Code)), "native-B")
			})
			b.Run("execute/"+name, func(b *testing.B) {
				cm := compileIntegerPressure(b, raw, nil)
				e := prepareIntegerPressure(b, cm, live)
				in := [3]uint64{0x8123456789abcdef, 7, 3}
				want := p.expected(in)
				e.input(in)
				if err := e.call(); err != nil {
					b.Fatal(err)
				}
				if err := e.check(p, in, want, 1); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := e.call(); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if err := e.check(p, in, want, uint32(b.N)+1); err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(cm.Code)), "native-B")
			})
		}
	}
}

// GNU objdump is an independent decoder, not an allocator/source proof. This
// checks emitted arithmetic placement; it does not locate individual spills.
func integerPressureNativePrefix(disassembly string, start, end, target, live int) (int, int, error) {
	divisions, shifts, calls := 0, 0, 0
	beforeDiv, beforeShift := 0, 0
	for _, line := range strings.Split(disassembly, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasSuffix(f[0], ":") {
			continue
		}
		addr, err := strconv.ParseUint(strings.TrimSuffix(f[0], ":"), 16, 64)
		if err != nil || int(addr) < start || int(addr) >= end {
			continue
		}
		switch f[1] {
		case "div", "idiv":
			divisions++
		case "shl", "shr", "sar", "rol", "ror":
			shifts++
		case "call", "callq":
			if len(f) < 3 {
				continue
			}
			dest, err := strconv.ParseUint(f[2], 0, 64)
			if err == nil && int(dest) == target {
				calls++
				beforeDiv = divisions
				beforeShift = shifts
			}
		}
	}
	if calls != 1 {
		return beforeDiv, beforeShift, fmt.Errorf("native direct-call count %d, want 1", calls)
	}
	if beforeDiv != live || beforeShift != live {
		return beforeDiv, beforeShift, fmt.Errorf("arithmetic before call: divisions=%d shifts=%d live=%d", beforeDiv, beforeShift, live)
	}
	return beforeDiv, beforeShift, nil
}

func TestIntegerPressureNativePrefix(t *testing.T) {
	tool, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("GNU objdump unavailable: native-prefix qualification not run")
	}
	version, err := exec.Command(tool, "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU objdump") {
		t.Skip("GNU objdump required for native-prefix qualification")
	}
	path := filepath.Join(t.TempDir(), "pressure.bin")
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 6, 8, 12, 24} {
			for seed := 0; seed < 3; seed++ {
				p := integerPressureCase{width, live, seed, true}
				cm := compileIntegerPressure(t, p.raw(), nil)
				if err := os.WriteFile(path, cm.Code, 0600); err != nil {
					t.Fatal(err)
				}
				out, err := exec.Command(tool, "-D", "--no-show-raw-insn", "-b", "binary", "-m", "i386:x86-64", path).Output()
				if err != nil {
					t.Fatal(err)
				}
				div, shift, err := integerPressureNativePrefix(string(out), cm.Entry[0], cm.Entry[1], cm.InternalEntry[1], live)
				if err != nil {
					t.Fatalf("i%d/live=%d/seed=%d: %v", width, live, seed, err)
				}
				t.Logf("i%d/live=%d/seed=%d native=%x caller=[%d,%d) division-before-call=%d shift-before-call=%d", width, live, seed, sha256.Sum256(cm.Code), cm.Entry[0], cm.Entry[1], div, shift)
			}
		}
	}
	// A valid straight-line substitute performs the same work without a call.
	p := integerPressureCase{64, 24, 1, false}
	cm := compileIntegerPressure(t, p.raw(), nil)
	if err := os.WriteFile(path, cm.Code, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tool, "-D", "--no-show-raw-insn", "-b", "binary", "-m", "i386:x86-64", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := integerPressureNativePrefix(string(out), cm.Entry[0], cm.Entry[1], cm.InternalEntry[1], p.live); err == nil || !strings.Contains(err.Error(), "native direct-call count 0") {
		t.Fatalf("native no-call control: %v", err)
	}
	if _, _, err := integerPressureNativePrefix("0: div %rcx\n3: div %rcx\n6: shl %cl,%rax\n9: call 0x10", 0, 16, 16, 1); err == nil {
		t.Fatal("extra arithmetic accepted")
	}
	if _, _, err := integerPressureNativePrefix("0: div %rcx\n3: shl %cl,%rax\n6: call 0x10", 6, 16, 16, 1); err == nil {
		t.Fatal("outside-caller arithmetic accepted")
	}
	// Decoder-output controls qualify the counting/ordering gate only.
	for _, text := range []string{"0: call 0x10\n5: div %rcx\n8: shl %cl,%rax", "0: div %rcx\n3: shl %cl,%rax\n6: call 0x20"} {
		if _, _, err := integerPressureNativePrefix(text, 0, 16, 16, 1); err == nil {
			t.Fatal("wrong target/order accepted")
		}
	}
}
