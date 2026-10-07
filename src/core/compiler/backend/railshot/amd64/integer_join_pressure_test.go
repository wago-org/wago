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

const (
	joinPrefixMarker = 0x1a2b3c4d
	joinThenMarker   = 0x2b3c4d5e
	joinElseMarker   = 0x3c4d5e6f
	joinEndMarker    = 0x4d5e6f70
)

type joinPressureCase struct {
	width, live, seed int
	straight          bool
}

func (p joinPressureCase) raw() []byte {
	typ, constant, xor, add, div, shift := i32, byte(0x41), byte(0x73), byte(0x6a), byte(0x6d), byte(0x74)
	if p.width == 64 {
		typ, constant, xor, add, div, shift = i64, 0x42, 0x85, 0x7c, 0x7f, 0x86
	}
	results := make([]wasm.ValType, p.live+1)
	for i := range results {
		results[i] = typ
	}
	body := []byte{0}
	marker := func(offset byte, value int32) {
		body = append(body, 0x41, offset, 0x41)
		body = append(body, wasmtest.SLEB32(value)...)
		body = append(body, 0x36, 2, 0)
	}
	for i := 0; i < p.live; i++ {
		salt := byte(1 + (i+p.seed*7)%31)
		body = append(body, 0x20, 0, constant, salt, xor, 0x20, 2, div+byte((i+p.seed)%4), 0x20, 1, shift+byte((i+p.seed)%5), constant, byte(i+1), add)
	}
	marker(0, joinPrefixMarker)
	arm := func(then bool) {
		mark, salt := int32(joinElseMarker), byte(0x29)
		if then {
			mark, salt = joinThenMarker, 0x35
		}
		marker(4, mark)
		// Count the selected arm independently of its returned value.
		body = append(body, 0x41, 8, 0x41, 8, 0x28, 2, 0, 0x41, 1, 0x6a, 0x36, 2, 0)
		body = append(body, 0x20, 0, constant, salt, xor)
	}
	if p.straight {
		arm(true)
	} else {
		blockType := byte(0x7f)
		if p.width == 64 {
			blockType = 0x7e
		}
		body = append(body, 0x20, 3, 0x04, blockType)
		arm(true)
		body = append(body, 0x05)
		arm(false)
		body = append(body, 0x0b)
	}
	marker(12, joinEndMarker)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ, i32}, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}
func (p joinPressureCase) expected(in [4]uint64) []uint64 {
	out := make([]uint64, p.live+1)
	for i := 0; i < p.live; i++ {
		x := in[0] ^ uint64(1+(i+p.seed*7)%31)
		x = divideResult(p.width, (i+p.seed)%4, x, in[2])
		out[i] = shiftResult(p.width, (i+p.seed)%5, x, in[1]) + uint64(i+1)
	}
	salt := uint64(0x29)
	if uint32(in[3]) != 0 {
		salt = 0x35
	}
	out[p.live] = in[0] ^ salt
	if p.width == 32 {
		for i := range out {
			out[i] = uint64(uint32(out[i]))
		}
	}
	return out
}
func joinPressureInputs() [][4]uint64 {
	inputs := make([][4]uint64, 0, 160)
	for _, x := range []uint64{0, 1, ^uint64(0), 1 << 31, 1 << 63, 0x8123456789abcdef} {
		for _, count := range shiftBoundaryCounts {
			divisor := uint64(3)
			if count&1 != 0 {
				divisor = ^uint64(2)
			} // -3: no zero or signed-overflow division
			for _, cond := range []uint64{0, 1, 0xffffffff, 0x100000000} {
				inputs = append(inputs, [4]uint64{x, count, divisor, cond})
			}
		}
	}
	state := uint64(810)
	for i := 0; i < 16; i++ {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		inputs = append(inputs, [4]uint64{state, state >> 32, 3, uint64(i % 2)})
	}
	return inputs
}
func compileJoinPressure(tb testing.TB, raw []byte, stats *ModuleStats) *enc.CompiledModule {
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

type joinPressureExecutor struct {
	engine                       *cr.Engine
	memory                       *cr.JobMemory
	entry                        uintptr
	args, results, guarded, trap []byte
	base                         uintptr
}

func prepareJoinPressure(tb testing.TB, cm *enc.CompiledModule, live int) *joinPressureExecutor {
	tb.Helper()
	e := new(joinPressureExecutor)
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
	e.guarded = ar.Alloc((live+1)*8 + 32)
	e.results = e.guarded[16 : len(e.guarded)-16]
	e.trap = ar.Alloc(cr.TrapBufferBytes)
	if err = e.memory.BindTrapCell(e.trap); err != nil {
		tb.Fatal(err)
	}
	e.base = e.memory.LinMemBase()
	return e
}
func (e *joinPressureExecutor) input(in [4]uint64) {
	for i, x := range in {
		binary.LittleEndian.PutUint64(e.args[i*8:], x)
	}
	for i := range e.guarded {
		e.guarded[i] = 0xa5
	}
	clear(e.memory.LinearMemory()[:32])
}
func (e *joinPressureExecutor) call() error {
	return e.engine.CallPrepared(e.entry, e.args, e.base, e.trap, e.results)
}
func (e *joinPressureExecutor) check(p joinPressureCase, in [4]uint64, want []uint64, calls uint32) error {
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
	if got := binary.LittleEndian.Uint32(e.memory.LinearMemory()[8:]); got != calls {
		return fmt.Errorf("work count %d, want %d", got, calls)
	}
	branch := uint32(joinElseMarker)
	if uint32(in[3]) != 0 {
		branch = joinThenMarker
	}
	for _, mark := range []struct {
		offset int
		want   uint32
	}{{0, joinPrefixMarker}, {4, branch}, {12, joinEndMarker}} {
		if got := binary.LittleEndian.Uint32(e.memory.LinearMemory()[mark.offset:]); got != mark.want {
			return fmt.Errorf("marker offset %d = %x, want %x", mark.offset, got, mark.want)
		}
	}
	return nil
}

func TestIntegerLiveAcrossJoinPressure(t *testing.T) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 8, 24} {
			for seed := 0; seed < 2; seed++ {
				p := joinPressureCase{width: width, live: live, seed: seed}
				t.Run(fmt.Sprintf("i%d/live=%d/seed=%d", width, live, seed), func(t *testing.T) {
					var stats *ModuleStats
					if diagnosticsEnabled {
						stats = new(ModuleStats)
					}
					raw := p.raw()
					cm := compileJoinPressure(t, raw, stats)
					if stats != nil {
						if len(stats.Funcs) != 1 {
							t.Fatal("missing function diagnostics")
						}
						f := stats.Funcs[0]
						for _, calls := range f.Calls {
							if calls != 0 {
								t.Fatal("unexpected call", f.Calls)
							}
						}
						if live == 24 && (f.Spills == 0 || f.Reloads == 0) {
							t.Fatalf("high-pressure fixture did not spill/reload: %+v", f)
						}
						t.Logf("shared=%t code=%d frame=%d spills=%d reloads=%d", f.SharedScalar, f.CodeBytes, f.FrameBytes, f.Spills, f.Reloads)
					} else {
						t.Log("pressure/path diagnostics unavailable; no transport-proof claim")
					}
					t.Logf("source=%x native=%x selected-cpu=0 required-cpu=%d bounds=explicit api=Engine.CallPrepared", sha256.Sum256(raw), sha256.Sum256(cm.Code), cm.RequiredAMD64Features)
					e := prepareJoinPressure(t, cm, live)
					for _, in := range joinPressureInputs() {
						e.input(in)
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
func TestJoinPressureObserverControls(t *testing.T) {
	p := joinPressureCase{width: 64, live: 24, seed: 1}
	e := prepareJoinPressure(t, compileJoinPressure(t, p.raw(), nil), p.live)
	in := [4]uint64{0x8123456789abcdef, 7, 3, 1}
	want := p.expected(in)
	e.input(in)
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
		e.results[i*8] ^= 1
		reject(fmt.Sprintf("slot %d", i))
		e.results[i*8] ^= 1
	}
	for _, offset := range []int{0, len(e.guarded) - 1} {
		e.guarded[offset] ^= 1
		reject("guard")
		e.guarded[offset] ^= 1
	}
	e.args[0] ^= 1
	reject("input")
	e.args[0] ^= 1
	for _, offset := range []int{0, 4, 8, 12} {
		e.memory.LinearMemory()[offset] ^= 1
		category := "marker"
		if offset == 8 {
			category = "work count"
		}
		reject(category)
		e.memory.LinearMemory()[offset] ^= 1
	}
	// Execute real wrong-arm work; an expected false arm must reject it.
	falseInput := in
	falseInput[3] = 0
	if err := e.check(p, in, p.expected(falseInput), 1); err == nil || !strings.Contains(err.Error(), "slot 24") {
		t.Fatalf("wrong arm accepted: %v", err)
	}
	e.input(in) // Omitted invocation leaves poisoned results and zero work.
	reject("slot")
}
func BenchmarkIntegerLiveAcrossJoin(b *testing.B) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 24} {
			p := joinPressureCase{width: width, live: live, seed: 1}
			raw := p.raw()
			name := fmt.Sprintf("i%d/live=%d", width, live)
			b.Run("compile/"+name, func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				var cm *enc.CompiledModule
				for i := 0; i < b.N; i++ {
					cm = compileJoinPressure(b, raw, nil)
				}
				b.StopTimer()
				b.ReportMetric(float64(len(cm.Code)), "native-B")
			})
			for _, cond := range []uint64{0, 1} {
				b.Run(fmt.Sprintf("execute/%s/arm=%d", name, cond), func(b *testing.B) {
					cm := compileJoinPressure(b, raw, nil)
					e := prepareJoinPressure(b, cm, live)
					in := [4]uint64{0x8123456789abcdef, 7, 3, cond}
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
}

// GNU objdump qualifies emitted placement and branch shape, not individual
// spill lifetimes or the independent source/transport proofs tracked by #807.
func joinPressureNativeEvidence(disassembly string, start, end, live int) error {
	type instruction struct {
		address  int
		op, args string
	}
	var code []instruction
	addresses := make(map[int]bool)
	markers := []string{"$0x1a2b3c4d,", "$0x2b3c4d5e,", "$0x3c4d5e6f,", "$0x4d5e6f70,"}
	positions := [4]int{-1, -1, -1, -1}
	for _, line := range strings.Split(disassembly, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[0], ":") {
			continue
		}
		address, err := strconv.ParseUint(strings.TrimSuffix(fields[0], ":"), 16, 64)
		if err != nil || int(address) < start || int(address) >= end {
			continue
		}
		args := strings.Join(fields[2:], "")
		if fields[1] == "call" || fields[1] == "callq" {
			return fmt.Errorf("unexpected native call")
		}
		code = append(code, instruction{int(address), fields[1], args})
		addresses[int(address)] = true
		for i, marker := range markers {
			if strings.HasPrefix(args, marker) && strings.HasPrefix(fields[1], "mov") {
				if positions[i] >= 0 {
					return fmt.Errorf("duplicate marker %d", i)
				}
				positions[i] = len(code) - 1
			}
		}
	}
	for i, pos := range positions {
		if pos < 0 || (i > 0 && pos <= positions[i-1]) {
			return fmt.Errorf("missing or reordered native marker %d", i)
		}
	}
	target := func(ins instruction) int {
		v, err := strconv.ParseUint(ins.args, 0, 64)
		if err != nil {
			return -1
		}
		return int(v)
	}
	// The exit skips the alternate arm marker and targets an instruction before
	// the end marker. This is a bounded shape check, not a full CFG proof.
	skip := -1
	for i := positions[1] + 1; i < positions[2]; i++ {
		dest := target(code[i])
		if code[i].op == "jmp" && dest > code[positions[2]].address && dest <= code[positions[3]].address {
			if !addresses[dest] {
				return fmt.Errorf("arm exit target is not an instruction")
			}
			if skip >= 0 {
				return fmt.Errorf("ambiguous arm exit")
			}
			skip = i
		}
	}
	if skip < 0 {
		return fmt.Errorf("missing native arm exit")
	}
	branches, branchIndex := 0, -1
	for i := positions[0] + 1; i < positions[1]; i++ {
		dest := target(code[i])
		if strings.HasPrefix(code[i].op, "j") && code[i].op != "jmp" && dest > code[skip].address && dest <= code[positions[2]].address {
			if !addresses[dest] {
				return fmt.Errorf("conditional target is not an instruction")
			}
			branches++
			branchIndex = i
		}
	}
	if branches != 1 {
		return fmt.Errorf("native conditional count %d want 1", branches)
	}
	divisions, shifts := 0, 0
	for i, ins := range code {
		arithmetic := false
		switch ins.op {
		case "div", "idiv":
			divisions++
			arithmetic = true
		case "shl", "shr", "sar", "rol", "ror":
			shifts++
			arithmetic = true
		}
		if arithmetic && i >= branchIndex {
			return fmt.Errorf("arithmetic after conditional branch")
		}
	}
	if divisions != live || shifts != live {
		return fmt.Errorf("prefix arithmetic divisions=%d shifts=%d want=%d", divisions, shifts, live)
	}
	return nil
}
func TestJoinPressureNativeEvidence(t *testing.T) {
	tool, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("GNU objdump unavailable: native join qualification not run")
	}
	version, err := exec.Command(tool, "--version").Output()
	if err != nil || !strings.Contains(string(version), "GNU objdump") {
		t.Skip("GNU objdump required for native join qualification")
	}
	path := filepath.Join(t.TempDir(), "join.bin")
	disassemble := func(p joinPressureCase) (*enc.CompiledModule, string) {
		t.Helper()
		cm := compileJoinPressure(t, p.raw(), nil)
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
				p := joinPressureCase{width: width, live: live, seed: seed}
				cm, out := disassemble(p)
				if err := joinPressureNativeEvidence(out, cm.InternalEntry[0], len(cm.Code), live); err != nil {
					t.Fatalf("i%d/live=%d/seed=%d: %v\n%s", width, live, seed, err, out)
				}
				t.Logf("i%d/live=%d/seed=%d native=%x prefix-divisions=%d prefix-shifts=%d conditional=1", width, live, seed, sha256.Sum256(cm.Code), live, live)
			}
		}
	}
	p := joinPressureCase{width: 64, live: 24, seed: 1, straight: true}
	cm, out := disassemble(p)
	e := prepareJoinPressure(t, cm, p.live)
	in := [4]uint64{0x8123456789abcdef, 7, 3, 1}
	e.input(in)
	if err := e.call(); err != nil {
		t.Fatal(err)
	}
	if err := e.check(p, in, p.expected(in), 1); err != nil {
		t.Fatal(err)
	}
	if err := joinPressureNativeEvidence(out, cm.InternalEntry[0], len(cm.Code), p.live); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("straight-line control: %v", err)
	}
}
func TestJoinPressureNativeObserverControls(t *testing.T) {
	valid := "0: div %rcx\n3: shl %cl,%rax\n6: movl $0x1a2b3c4d,(%rsi)\n10: je 0x30\n15: movl $0x2b3c4d5e,4(%rsi)\n25: jmp 0x40\n30: movl $0x3c4d5e6f,4(%rsi)\n40: movl $0x4d5e6f70,12(%rsi)"
	if err := joinPressureNativeEvidence(valid, 0, 0x50, 1); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, text, category string
		start                int
	}{
		{"missing division", strings.Replace(valid, "div %rcx", "nop", 1), "prefix arithmetic", 0},
		{"extra arithmetic", strings.Replace(valid, "3: shl", "2: div %rcx\n3: shl", 1), "prefix arithmetic", 0},
		{"late arithmetic", valid + "\n48: div %rcx", "arithmetic after conditional", 0},
		{"outside function", valid, "prefix arithmetic", 3},
		{"wrong target", strings.Replace(valid, "je 0x30", "je 0x40", 1), "native conditional count", 0},
		{"conditional middle", strings.Replace(valid, "je 0x30", "je 0x29", 1), "conditional target", 0},
		{"exit middle", strings.Replace(valid, "jmp 0x40", "jmp 0x39", 1), "arm exit target", 0},
		{"no join", strings.Replace(valid, "jmp 0x40", "nop", 1), "missing native arm exit", 0},
		{"call", valid + "\n48: call 0x60", "unexpected native call", 0},
		{"duplicate marker", valid + "\n48: movl $0x1a2b3c4d,(%rsi)", "duplicate marker", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := joinPressureNativeEvidence(tc.text, tc.start, 0x50, 1); err == nil || !strings.Contains(err.Error(), tc.category) {
				t.Fatalf("%s control: %v", tc.category, err)
			}
		})
	}
}
