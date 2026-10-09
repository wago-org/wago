//go:build linux && amd64 && !tinygo

package amd64

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	cr "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const loopBodyMarker = 0x2b3c4d5e
const loopEndMarker = 0x4d5e6f70

type loopPressureCase struct {
	width, live, seed int
	once              bool
}

func (p loopPressureCase) raw() []byte {
	typ, blockType, constant, xor, add, div, shift, store, align := i32, byte(0x7f), byte(0x41), byte(0x73), byte(0x6a), byte(0x6d), byte(0x74), byte(0x36), byte(2)
	if p.width == 64 {
		typ, blockType, constant, xor, add, div, shift, store, align = i64, 0x7e, 0x42, 0x85, 0x7c, 0x7f, 0x86, 0x37, 3
	}
	body := []byte{1, byte(p.live), blockType}
	marker := func(offset byte, value int32) {
		body = append(body, 0x41, offset, 0x41)
		body = append(body, wasmtest.SLEB32(value)...)
		body = append(body, 0x36, 2, 0)
	}
	increment := func(offset byte) {
		body = append(body, 0x41, offset, 0x41, offset, 0x28, 2, 0, 0x41, 1, 0x6a, 0x36, 2, 0)
	}
	increment(12)                                        // Invocation count also qualifies zero-iteration calls.
	body = append(body, 0x20, 3, 0x41, 7, 0x71, 0x21, 3) // Bound every input to 0..7 iterations.
	for i := 0; i < p.live; i++ {
		body = append(body, 0x20, 0, constant, byte(i+1), xor, 0x21, byte(4+i))
	}
	body = append(body, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x45, 0x0d, 1)
	marker(4, loopBodyMarker)
	// Compute every next state before replacing any current state. The division
	// and variable shift results coexist as transient operands inside the loop.
	for i := 0; i < p.live; i++ {
		body = append(body, 0x20, byte(4+i), constant, byte(1+(i+p.seed*7)%31), xor, 0x20, 2, div+byte((i+p.seed)%4), 0x20, 1, shift+byte((i+p.seed)%5), constant, byte(i+1), add)
	}
	// A short conditional materializes the computed stack prefix before the
	// next-state local assignments. Without it, local condensation can stream
	// the updates and remove the intended transient pressure.
	body = append(body, 0x20, 3, 0x41, 1, 0x71, 0x04, 0x40)
	marker(20, 0x1a2b3c4d)
	increment(24)
	body = append(body, 0x05)
	marker(20, 0x1a2b3c4e)
	increment(28)
	body = append(body, 0x0b)
	for i := p.live - 1; i >= 0; i-- {
		body = append(body, 0x21, byte(4+i))
	}
	increment(8)
	body = append(body, 0x20, 3, 0x41, 1, 0x6b, 0x21, 3)
	if !p.once {
		body = append(body, 0x0c, 0)
	}
	body = append(body, 0x0b, 0x0b)
	marker(16, loopEndMarker)
	for i := 0; i < p.live; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(64+8*i))...)
		body = append(body, 0x20, byte(4+i), store, align, 0)
	}
	body = append(body, 0x41, 8, 0x28, 2, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ, i32}, []wasm.ValType{i32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}
func (p loopPressureCase) expected(in [4]uint64) []uint64 {
	out := make([]uint64, p.live)
	for i := range out {
		out[i] = in[0] ^ uint64(i+1)
		if p.width == 32 {
			out[i] = uint64(uint32(out[i]))
		}
	}
	for n := uint32(in[3]) & 7; n > 0; n-- {
		for i, x := range out {
			x ^= uint64(1 + (i+p.seed*7)%31)
			x = divideResult(p.width, (i+p.seed)%4, x, in[2])
			out[i] = shiftResult(p.width, (i+p.seed)%5, x, in[1]) + uint64(i+1)
			if p.width == 32 {
				out[i] = uint64(uint32(out[i]))
			}
		}
	}
	return out
}
func loopPressureInputs() [][4]uint64 {
	var inputs [][4]uint64
	for _, x := range []uint64{0, 1, ^uint64(0), 1 << 31, 1 << 63, 0x8123456789abcdef} {
		for _, count := range shiftBoundaryCounts {
			divisor := uint64(3)
			if count&1 != 0 {
				divisor = ^uint64(2)
			}
			for _, n := range []uint64{0, 1, 2, 7, 0x100000008} {
				inputs = append(inputs, [4]uint64{x, count, divisor, n})
			}
		}
	}
	return inputs
}
func compileLoopPressure(tb testing.TB, raw []byte, stats *ModuleStats) *enc.CompiledModule {
	tb.Helper()
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		tb.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: 0,
		Stats: stats})
	if err != nil {
		tb.Fatal(err)
	}
	return cm
}

type loopPressureExecutor struct {
	engine                       *cr.Engine
	memory                       *cr.JobMemory
	entry                        uintptr
	args, results, guarded, trap []byte
	base                         uintptr
}

func prepareLoopPressure(tb testing.TB, cm *enc.CompiledModule) *loopPressureExecutor {
	tb.Helper()
	e := new(loopPressureExecutor)
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
	e.args = ar.Alloc(32)
	e.guarded = ar.Alloc(40)
	e.results = e.guarded[16 : len(e.guarded)-16]
	e.trap = ar.Alloc(cr.TrapBufferBytes)
	if err = e.memory.BindTrapCell(e.trap); err != nil {
		tb.Fatal(err)
	}
	e.base = e.memory.LinMemBase()
	return e
}

func (e *loopPressureExecutor) input(p loopPressureCase, in [4]uint64) {
	for i, x := range in {
		binary.LittleEndian.PutUint64(e.args[i*8:], x)
	}
	for i := range e.guarded {
		e.guarded[i] = 0xa5
	}
	mem := e.memory.LinearMemory()
	for i := range mem[:64+p.live*8+16] {
		mem[i] = 0xa5
	}
	clear(mem[8:16])
	clear(mem[24:32])
}
func (e *loopPressureExecutor) call() error {
	return e.engine.CallPrepared(e.entry, e.args, e.base, e.trap, e.results)
}
func (e *loopPressureExecutor) check(p loopPressureCase, in [4]uint64, want []uint64, calls uint32) error {
	mem := e.memory.LinearMemory()
	iterations := uint32(in[3]) & 7
	if got := binary.LittleEndian.Uint32(mem[12:]); got != calls {
		return fmt.Errorf("invocations %d want %d", got, calls)
	}
	if got := binary.LittleEndian.Uint32(mem[8:]); got != calls*iterations {
		return fmt.Errorf("iterations %d want %d", got, calls*iterations)
	}
	for _, arm := range []struct {
		offset int
		count  uint32
	}{{24, (iterations + 1) / 2}, {28, iterations / 2}} {
		if got := binary.LittleEndian.Uint32(mem[arm.offset:]); got != calls*arm.count {
			return fmt.Errorf("arm count offset %d = %d want %d", arm.offset, got, calls*arm.count)
		}
	}
	if got := binary.LittleEndian.Uint32(e.results); got != calls*iterations {
		return fmt.Errorf("return %d want %d", got, calls*iterations)
	}
	for i, x := range want {
		got := binary.LittleEndian.Uint64(mem[64+8*i:])
		if p.width == 32 {
			got = uint64(uint32(got))
			if binary.LittleEndian.Uint32(mem[68+8*i:]) != 0xa5a5a5a5 {
				return fmt.Errorf("slot padding %d overwritten", i)
			}
		}
		if got != x {
			return fmt.Errorf("slot %d = %x want %x", i, got, x)
		}
	}
	for i, x := range in {
		if binary.LittleEndian.Uint64(e.args[i*8:]) != x {
			return fmt.Errorf("input %d changed", i)
		}
	}
	for _, guard := range [][]byte{e.guarded[:16], e.guarded[len(e.guarded)-16:], mem[48:64], mem[64+8*p.live : 64+8*p.live+16]} {
		for _, x := range guard {
			if x != 0xa5 {
				return fmt.Errorf("guard overwritten")
			}
		}
	}
	body := uint32(0xa5a5a5a5)
	if iterations > 0 {
		body = loopBodyMarker
	}
	armMarker := uint32(0xa5a5a5a5)
	if iterations > 0 {
		armMarker = 0x1a2b3c4d
	}
	if binary.LittleEndian.Uint32(mem[4:]) != body || binary.LittleEndian.Uint32(mem[16:]) != loopEndMarker || binary.LittleEndian.Uint32(mem[20:]) != armMarker {
		return fmt.Errorf("marker mismatch")
	}
	return nil
}
func TestIntegerLoopCarriedPressure(t *testing.T) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 8, 24} {
			for seed := 0; seed < 2; seed++ {
				p := loopPressureCase{width: width, live: live, seed: seed}
				t.Run(fmt.Sprintf("i%d/live=%d/seed=%d", width, live, seed), func(t *testing.T) {
					var stats *ModuleStats
					if diagnosticsEnabled {
						stats = new(ModuleStats)
					}
					raw := p.raw()
					cm := compileLoopPressure(t, raw, stats)
					if stats != nil {
						if len(stats.Funcs) != 1 {
							t.Fatal("missing diagnostics")
						}
						f := stats.Funcs[0]
						if live == 24 && (f.Spills == 0 || f.Reloads == 0) {
							t.Fatalf("high pressure not admitted: %+v", f)
						}
						t.Logf("shared=%t native=%d frame=%d spills=%d reloads=%d pins=%d interval-control=%d", f.SharedScalar, f.CodeBytes, f.FrameBytes, f.Spills, f.Reloads, f.PinnedLocals, f.Peephole["interval-control"])
					} else {
						t.Log("spill/path counters unavailable; no source/transport proof claim")
					}
					t.Logf("source=%x native=%x selected-cpu=0 required-cpu=%d bounds=explicit api=Engine.CallPrepared", sha256.Sum256(raw), sha256.Sum256(cm.Code), cm.RequiredAMD64Features)
					e := prepareLoopPressure(t, cm)
					for _, in := range loopPressureInputs() {
						e.input(p, in)
						if err := e.call(); err != nil {
							t.Fatal(err)
						}
						if err := e.check(p, in, p.expected(in), 1); err != nil {
							t.Fatalf("input=%x: %v", in, err)
						}
					}
				})
			}
		}
	}
}
func TestLoopPressureObserverControls(t *testing.T) {
	for _, width := range []int{32, 64} {
		p := loopPressureCase{width: width, live: 24, seed: 1}
		e := prepareLoopPressure(t, compileLoopPressure(t, p.raw(), nil))
		in := [4]uint64{0x8123456789abcdef, 7, 3, 7}
		want := p.expected(in)
		e.input(p, in)
		if err := e.call(); err != nil {
			t.Fatal(err)
		}
		if err := e.check(p, in, want, 1); err != nil {
			t.Fatal(err)
		}
		reject := func(category string) {
			t.Helper()
			if err := e.check(p, in, want, 1); err == nil || !strings.Contains(err.Error(), category) {
				t.Fatalf("%s control: %v", category, err)
			}
		}
		for i := range want {
			e.memory.LinearMemory()[64+i*8] ^= 1
			reject(fmt.Sprintf("slot %d", i))
			e.memory.LinearMemory()[64+i*8] ^= 1
		}
		if width == 32 {
			e.memory.LinearMemory()[68] ^= 1
			reject("slot padding")
			e.memory.LinearMemory()[68] ^= 1
		}
		for _, tc := range []struct {
			offset   int
			category string
		}{{4, "marker"}, {8, "iterations"}, {12, "invocations"}, {16, "marker"}, {20, "marker"}, {24, "arm count"}, {28, "arm count"}, {48, "guard"}, {64 + p.live*8, "guard"}} {
			e.memory.LinearMemory()[tc.offset] ^= 1
			reject(tc.category)
			e.memory.LinearMemory()[tc.offset] ^= 1
		}
		e.args[0] ^= 1
		reject("input")
		e.args[0] ^= 1
		e.results[0] ^= 1
		reject("return")
		e.results[0] ^= 1
		for _, offset := range []int{0, len(e.guarded) - 1} {
			e.guarded[offset] ^= 1
			reject("guard")
			e.guarded[offset] ^= 1
		}
		if err := e.call(); err != nil {
			t.Fatal(err)
		}
		reject("invocations")
		e.input(p, in)
		reject("invocations")
		// The real one-pass substitute agrees for one iteration, but fails the
		// independent work count for a request that requires actual backedges.
		p.once = true
		e = prepareLoopPressure(t, compileLoopPressure(t, p.raw(), nil))
		for _, n := range []uint64{1, 7} {
			in[3] = n
			e.input(p, in)
			if err := e.call(); err != nil {
				t.Fatal(err)
			}
			err := e.check(p, in, p.expected(in), 1)
			if n == 1 && err != nil {
				t.Fatal(err)
			}
			if n == 7 && (err == nil || !strings.Contains(err.Error(), "iterations")) {
				t.Fatalf("missing backedge control: %v", err)
			}
		}
	}
}
func BenchmarkIntegerLoopCarried(b *testing.B) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 24} {
			p := loopPressureCase{width: width, live: live, seed: 1}
			raw := p.raw()
			name := fmt.Sprintf("i%d/live=%d", width, live)
			b.Run("compile/"+name, func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				var cm *enc.CompiledModule
				for i := 0; i < b.N; i++ {
					cm = compileLoopPressure(b, raw, nil)
				}
				b.StopTimer()
				b.ReportMetric(float64(len(cm.Code)), "native-B")
			})
			b.Run("execute/"+name, func(b *testing.B) {
				cm := compileLoopPressure(b, raw, nil)
				e := prepareLoopPressure(b, cm)
				in := [4]uint64{0x8123456789abcdef, 7, 3, 7}
				want := p.expected(in)
				e.input(p, in)
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
				b.ReportMetric(7, "guest-iterations/op")
			})
		}
	}
}

// Qualify the bounded loop shape using an independent decoder. Frame traffic
// inside the loop is observable; identifying individual spill lifetimes is not.
func loopPressureNativeEvidence(text string, start, end, live int) (int, int, error) {
	type instruction struct {
		address  int
		op, args string
	}
	var code []instruction
	addresses := make(map[int]int)
	bodyMark, endMark := -1, -1
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasSuffix(f[0], ":") {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(f[0], ":"), 16, 64)
		if err != nil || int(n) < start || int(n) >= end {
			continue
		}
		args := strings.Join(f[2:], "")
		if f[1] == "call" || f[1] == "callq" {
			return 0, 0, fmt.Errorf("unexpected native call")
		}
		if strings.HasPrefix(f[1], "mov") {
			if strings.HasPrefix(args, "$0x2b3c4d5e,") {
				if bodyMark >= 0 {
					return 0, 0, fmt.Errorf("duplicate body marker")
				}
				bodyMark = len(code)
			}
			if strings.HasPrefix(args, "$0x4d5e6f70,") {
				if endMark >= 0 {
					return 0, 0, fmt.Errorf("duplicate end marker")
				}
				endMark = len(code)
			}
		}
		addresses[int(n)] = len(code)
		code = append(code, instruction{int(n), f[1], args})
	}
	if bodyMark < 0 || endMark <= bodyMark {
		return 0, 0, fmt.Errorf("missing loop markers")
	}
	target := func(ins instruction) int {
		n, err := strconv.ParseUint(ins.args, 0, 64)
		if err != nil {
			return -1
		}
		return int(n)
	}
	back := -1
	for i := bodyMark + 1; i < endMark; i++ {
		dest := target(code[i])
		if strings.HasPrefix(code[i].op, "j") && dest >= start && dest <= code[bodyMark].address {
			if back >= 0 {
				return 0, 0, fmt.Errorf("ambiguous backedge")
			}
			_, ok := addresses[dest]
			if !ok {
				return 0, 0, fmt.Errorf("backedge target is not an instruction")
			}
			back = i
		}
	}
	if back < 0 {
		return 0, 0, fmt.Errorf("missing native backedge")
	}
	exits := 0
	for i := 0; i < bodyMark; i++ {
		dest := target(code[i])
		if strings.HasPrefix(code[i].op, "j") && code[i].op != "jmp" && dest > code[back].address && dest <= code[endMark].address {
			if _, ok := addresses[dest]; !ok {
				return 0, 0, fmt.Errorf("exit target is not an instruction")
			}
			exits++
		}
	}
	if exits != 1 {
		return 0, 0, fmt.Errorf("loop exit count %d want 1", exits)
	}
	divs, shifts, reads, writes := 0, 0, 0, 0
	for i, ins := range code {
		arithmetic := false
		switch ins.op {
		case "div", "idiv":
			divs++
			arithmetic = true
		case "shl", "shr", "sar", "rol", "ror":
			shifts++
			arithmetic = true
		}
		if arithmetic && (i <= bodyMark || i >= back) {
			return 0, 0, fmt.Errorf("arithmetic outside loop body")
		}
		if i > bodyMark && i < back && strings.HasPrefix(ins.op, "mov") {
			source, dest, ok := strings.Cut(ins.args, ",")
			if ok {
				if strings.Contains(source, "(%rsp)") {
					reads++
				}
				if strings.Contains(dest, "(%rsp)") {
					writes++
				}
			}
		}
	}
	if divs != live || shifts != live {
		return reads, writes, fmt.Errorf("loop arithmetic divisions=%d shifts=%d want=%d", divs, shifts, live)
	}
	if live == 24 && (reads < 8 || writes < 8) {
		return reads, writes, fmt.Errorf("in-loop frame traffic reads=%d writes=%d", reads, writes)
	}
	return reads, writes, nil
}
func TestLoopPressureNativeEvidence(t *testing.T) {
	tool, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("GNU objdump unavailable: native loop qualification not run")
	}
	version, err := exec.Command(tool, "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU objdump") {
		t.Skip("GNU objdump required for native loop qualification")
	}
	path := filepath.Join(t.TempDir(), "loop.bin")
	disassemble := func(p loopPressureCase) (*enc.CompiledModule, string) {
		t.Helper()
		cm := compileLoopPressure(t, p.raw(), nil)
		if err := os.WriteFile(path, cm.Code, 0600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(tool, "-D", "--no-show-raw-insn", "-b", "binary", "-m", "i386:x86-64", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		return cm, string(out)
	}
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 8, 24} {
			for seed := 0; seed < 2; seed++ {
				p := loopPressureCase{width: width, live: live, seed: seed}
				cm, out := disassemble(p)
				reads, writes, err := loopPressureNativeEvidence(out, cm.InternalEntry[0], len(cm.Code), live)
				if err != nil {
					t.Fatalf("i%d/live=%d/seed=%d: %v\n%s", width, live, seed, err, out)
				}
				t.Logf("i%d/live=%d/seed=%d native=%x loop-frame-reads=%d writes=%d divisions=%d shifts=%d", width, live, seed, sha256.Sum256(cm.Code), reads, writes, live, live)
			}
		}
	}
	p := loopPressureCase{width: 64, live: 24, seed: 1}
	cm, out := disassemble(p)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasPrefix(fields[1], "mov") && strings.Contains(strings.Join(fields[2:], ""), "(%rsp)") {
			lines[i] = fields[0] + " nop"
		}
	}
	if _, _, err := loopPressureNativeEvidence(strings.Join(lines, "\n"), cm.InternalEntry[0], len(cm.Code), p.live); err == nil || !strings.Contains(err.Error(), "in-loop frame traffic") {
		t.Fatalf("frame-traffic control: %v", err)
	}
	p.once = true

	cm, out = disassemble(p)
	if _, _, err := loopPressureNativeEvidence(out, cm.InternalEntry[0], len(cm.Code), p.live); err == nil || !strings.Contains(err.Error(), "missing native backedge") {
		t.Fatalf("one-pass native control: %v", err)
	}
}
func TestLoopPressureNativeObserverControls(t *testing.T) {
	valid := "0: test %ecx,%ecx\n3: je 0x40\n8: movl $0x2b3c4d5e,(%rbx)\n10: div %rdi\n13: shl %cl,%rax\n18: mov %rax,8(%rsp)\n20: mov 8(%rsp),%rax\n30: jmp 0x0\n40: movl $0x4d5e6f70,(%rbx)"
	if _, _, err := loopPressureNativeEvidence(valid, 0, 0x50, 1); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, text, category string
		start, live          int
	}{
		{"missing backedge", strings.Replace(valid, "jmp 0x0", "nop", 1), "missing native backedge", 0, 1},
		{"interior backedge", strings.Replace(valid, "jmp 0x0", "jmp 0x1", 1), "backedge target", 0, 1},
		{"interior exit", strings.Replace(valid, "je 0x40", "je 0x39", 1), "exit target", 0, 1},
		{"wrong exit", strings.Replace(valid, "je 0x40", "je 0x10", 1), "loop exit count", 0, 1},
		{"missing arithmetic", strings.Replace(valid, "div %rdi", "nop", 1), "loop arithmetic", 0, 1},
		{"late arithmetic", valid + "\n48: div %rdi", "arithmetic outside", 0, 1},
		{"call", valid + "\n48: call 0x60", "unexpected native call", 0, 1},
		{"outside function", valid, "missing native backedge", 8, 1},
		{"duplicate marker", valid + "\n48: movl $0x2b3c4d5e,(%rbx)", "duplicate body marker", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := loopPressureNativeEvidence(tc.text, tc.start, 0x50, tc.live); err == nil || !strings.Contains(err.Error(), tc.category) {
				t.Fatalf("%s control: %v", tc.category, err)
			}
		})
	}
}
