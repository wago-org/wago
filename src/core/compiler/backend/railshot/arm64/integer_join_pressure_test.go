//go:build linux && arm64 && !tinygo

package arm64

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	cr "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/bits"
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
	typ, constant, xor, add, div, shift := wasm.I32, byte(0x41), byte(0x73), byte(0x6a), byte(0x6d), byte(0x74)
	if p.width == 64 {
		typ, constant, xor, add, div, shift = wasm.I64, 0x42, 0x85, 0x7c, 0x7f, 0x86
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
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ, typ, wasm.I32}, results))),
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
	cm, err := CompileModuleWith(m, CompileOptions{Workers: 1, DeferCodeMapping: true, ElideBoundsChecks: false,
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
						// ARM64 result-slot loads do not increment the reload counter.
						if live == 24 && (f.Spills == 0 || f.MaxSpillSlots < live) {
							t.Fatalf("high-pressure fixture did not spill/reserve result slots: %+v", f)
						}
						t.Logf("shared=%t code=%d frame=%d spills=%d reloads=%d", f.SharedScalar, f.CodeBytes, f.FrameBytes, f.Spills, f.Reloads)
					} else {
						t.Log("pressure/path diagnostics unavailable; no transport-proof claim")
					}
					t.Logf("source=%x native=%x target=arm64 bounds=explicit api=Engine.CallPrepared", sha256.Sum256(raw), sha256.Sum256(cm.Code))
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

var shiftBoundaryCounts = [...]uint64{0, 31, 32, 63, 64, 127}

func shiftResult(width, op int, x, count uint64) uint64 {
	if width == 32 {
		x = uint64(uint32(x))
		count &= 31
		switch op {
		case 0:
			return uint64(uint32(x) << count)
		case 1:
			return uint64(uint32(int32(x) >> count))
		case 2:
			return x >> count
		case 3:
			return uint64(bits.RotateLeft32(uint32(x), int(count)))
		default:
			return uint64(bits.RotateLeft32(uint32(x), -int(count)))
		}
	}
	count &= 63
	switch op {
	case 0:
		return x << count
	case 1:
		return uint64(int64(x) >> count)
	case 2:
		return x >> count
	case 3:
		return bits.RotateLeft64(x, int(count))
	default:
		return bits.RotateLeft64(x, -int(count))
	}
}

func divideResult(width, op int, x, y uint64) uint64 {
	if width == 32 {
		switch op {
		case 0:
			return uint64(uint32(int32(x) / int32(y)))
		case 1:
			return uint64(uint32(x) / uint32(y))
		case 2:
			return uint64(uint32(int32(x) % int32(y)))
		default:
			return uint64(uint32(x) % uint32(y))
		}
	}
	switch op {
	case 0:
		return uint64(int64(x) / int64(y))
	case 1:
		return x / y
	case 2:
		return uint64(int64(x) % int64(y))
	default:
		return x % y
	}
}

// This bounded A64 observer checks placement and branch shape, not individual
// spill lifetimes or the independent source/transport proofs tracked by #807.
// A64 instructions are aligned four-byte words; no external disassembler is needed.
func joinPressureNativeEvidence(code []byte, start, end, live int) error {
	if start < 0 || end > len(code) || start >= end || start%4 != 0 || end%4 != 0 {
		return fmt.Errorf("invalid native instruction range")
	}
	word := func(at int) uint32 { return binary.LittleEndian.Uint32(code[at:]) }
	markers := [...]uint32{joinPrefixMarker, joinThenMarker, joinElseMarker, joinEndMarker}
	positions := [4]int{-1, -1, -1, -1}
	for at := start; at < end; at += 4 {
		w := word(at)
		if w&0xfc000000 == 0x94000000 || w&0xfffffc1f == 0xd63f0000 { // BL, BLR
			return fmt.Errorf("unexpected native call")
		}
		if at+8 > end || w&0x7fe00000 != 0x52800000 { // MOVZ W/X, low halfword
			continue
		}
		next := word(at + 4)
		if next&0x7fe00000 != 0x72a00000 || (w^next)&0x8000001f != 0 { // MOVK, same register/width, LSL #16
			continue
		}
		value := (w>>5)&0xffff | ((next>>5)&0xffff)<<16
		for i, marker := range markers {
			if value == marker {
				if positions[i] >= 0 {
					return fmt.Errorf("duplicate marker %d", i)
				}
				positions[i] = at
			}
		}
	}
	for i, at := range positions {
		if at < 0 || (i > 0 && at <= positions[i-1]) {
			return fmt.Errorf("missing or reordered native marker %d", i)
		}
	}
	branchTarget := func(at int) (target int, conditional, branch bool) {
		w := word(at)
		if w&0xfc000000 == 0x14000000 { // B, signed imm26 scaled by four
			return at + int(int32(w<<6)>>4), false, true
		}
		if w&0x7e000000 == 0x34000000 || w&0xff000010 == 0x54000000 { // CBZ/CBNZ, B.cond
			return at + int(int32(w<<8)>>11), true, true
		}
		return 0, false, false
	}
	// The then exit must skip the else marker and reach a complete instruction
	// at or before the end marker. This is not a complete control-flow proof.
	skip := -1
	for at := positions[1] + 8; at < positions[2]; at += 4 {
		dest, conditional, branch := branchTarget(at)
		if branch && !conditional && dest > positions[2] && dest <= positions[3] {
			if skip >= 0 {
				return fmt.Errorf("ambiguous arm exit")
			}
			skip = at
		}
	}
	if skip < 0 {
		return fmt.Errorf("missing native arm exit")
	}
	branches, branchAt := 0, -1
	for at := positions[0] + 8; at < positions[1]; at += 4 {
		dest, conditional, branch := branchTarget(at)
		if branch && conditional && dest > skip && dest <= positions[2] {
			branches++
			branchAt = at
		}
	}
	if branches != 1 {
		return fmt.Errorf("native conditional count %d want 1", branches)
	}
	divisions, shifts := 0, 0
	for at := start; at < end; at += 4 {
		arithmetic := false
		switch word(at) & 0x7fe0fc00 { // Ignore SF and operand registers.
		case 0x1ac00800, 0x1ac00c00: // UDIV, SDIV (also used for Wasm remainder)
			divisions++
			arithmetic = true
		case 0x1ac02000, 0x1ac02400, 0x1ac02800, 0x1ac02c00: // LSLV, LSRV, ASRV, RORV
			shifts++
			arithmetic = true
		}
		if arithmetic && at >= branchAt {
			return fmt.Errorf("arithmetic after conditional branch")
		}
	}
	if divisions != live || shifts != live {
		return fmt.Errorf("prefix arithmetic divisions=%d shifts=%d want=%d", divisions, shifts, live)
	}
	return nil
}

func TestJoinPressureNativeEvidence(t *testing.T) {
	for _, width := range []int{32, 64} {
		for _, live := range []int{1, 8, 24} {
			for seed := 0; seed < 2; seed++ {
				p := joinPressureCase{width: width, live: live, seed: seed}
				cm := compileJoinPressure(t, p.raw(), nil)
				if err := joinPressureNativeEvidence(cm.Code, cm.InternalEntry[0], len(cm.Code), live); err != nil {
					t.Fatalf("i%d/live=%d/seed=%d: %v\n%x", width, live, seed, err, cm.Code)
				}
				t.Logf("i%d/live=%d/seed=%d native=%x prefix-divisions=%d prefix-shifts=%d conditional=1", width, live, seed, sha256.Sum256(cm.Code), live, live)
			}
		}
	}
	p := joinPressureCase{width: 64, live: 24, seed: 1, straight: true}
	cm := compileJoinPressure(t, p.raw(), nil)
	e := prepareJoinPressure(t, cm, p.live)
	in := [4]uint64{0x8123456789abcdef, 7, 3, 1}
	e.input(in)
	if err := e.call(); err != nil {
		t.Fatal(err)
	}
	if err := e.check(p, in, p.expected(in), 1); err != nil {
		t.Fatal(err)
	}
	if err := joinPressureNativeEvidence(cm.Code, cm.InternalEntry[0], len(cm.Code), p.live); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("straight-line control: %v", err)
	}
}

func TestJoinPressureNativeObserverControls(t *testing.T) {
	// Hand-encoded A64 words keep the observer independent of Wago's encoder.
	// UDIV X0,X1,X2; LSLV X0,X0,X1; marker; CBZ W0,else; then;
	// B end; else; end. Branch offsets are relative to their own instruction.
	valid := []uint32{
		0x9ac20820, 0x9ac12000,
		0xd28789b1, 0xf2a34571, // 0x1a2b3c4d
		0x34000080,             // byte 16 -> 32
		0xd289abd1, 0xf2a56791, // 0x2b3c4d5e
		0x14000003,             // byte 28 -> 40
		0xd28bcdf1, 0xf2a789b1, // 0x3c4d5e6f
		0xd28dee11, 0xf2a9abd1, // 0x4d5e6f70
	}
	encode := func(words []uint32) []byte {
		code := make([]byte, len(words)*4)
		for i, w := range words {
			binary.LittleEndian.PutUint32(code[i*4:], w)
		}
		return code
	}
	if err := joinPressureNativeEvidence(encode(valid), 0, len(valid)*4, 1); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category string
		index          int
		word           uint32
	}{
		{"missing division", "prefix arithmetic", 0, 0xd503201f}, // NOP
		{"extra arithmetic", "prefix arithmetic", 1, 0x9ac20820},
		{"late arithmetic", "arithmetic after conditional", 12, 0x9ac12000},
		{"wrong target", "native conditional count", 4, 0x340000c0},         // -> end
		{"backward conditional", "native conditional count", 4, 0x34ffff80}, // -> byte 0
		{"backward exit", "missing native arm exit", 7, 0x17fffff9},         // -> byte 0
		{"no join", "missing native arm exit", 7, 0xd503201f},
		{"direct call", "unexpected native call", 12, 0x94000000},
		{"indirect call", "unexpected native call", 12, 0xd63f0000},
		{"wrong marker register", "marker", 3, 0xf2a34572},
		{"wrong marker width", "marker", 3, 0x72a34571},
		{"wrong marker halfword", "marker", 3, 0xf2c34571},
	} {
		t.Run(tc.name, func(t *testing.T) {
			words := append(append([]uint32(nil), valid...), 0xd503201f)
			words[tc.index] = tc.word
			if err := joinPressureNativeEvidence(encode(words), 0, len(words)*4, 1); err == nil || !strings.Contains(err.Error(), tc.category) {
				t.Fatalf("%s control: %v", tc.category, err)
			}
		})
	}
	for _, branch := range []uint32{0x35000080, 0x54000080} { // CBNZ, B.EQ -> else
		words := append([]uint32(nil), valid...)
		words[4] = branch
		if err := joinPressureNativeEvidence(encode(words), 0, len(words)*4, 1); err != nil {
			t.Fatalf("conditional %#x: %v", branch, err)
		}
	}
	// Accept both register widths and each counted arithmetic opcode.
	for _, sf := range []uint32{0, 0x80000000} {
		for _, div := range []uint32{0x1ac20820, 0x1ac20c20} {
			for _, shift := range []uint32{0x1ac12000, 0x1ac12400, 0x1ac12800, 0x1ac12c00} {
				words := append([]uint32(nil), valid...)
				words[0], words[1] = sf|div, sf|shift
				for _, i := range []int{2, 3, 5, 6, 8, 9, 10, 11} {
					words[i] = words[i]&0x7fffffff | sf
				}
				if err := joinPressureNativeEvidence(encode(words), 0, len(words)*4, 1); err != nil {
					t.Fatalf("sf=%#x div=%#x shift=%#x: %v", sf, div, shift, err)
				}
			}
		}
	}
	duplicate := append(append([]uint32(nil), valid...), valid[2:4]...)
	if err := joinPressureNativeEvidence(encode(duplicate), 0, len(duplicate)*4, 1); err == nil || !strings.Contains(err.Error(), "duplicate marker") {
		t.Fatalf("duplicate marker control: %v", err)
	}
	for _, bounds := range [][2]int{{-4, 48}, {0, 49}, {1, 48}, {0, 47}, {0, 0}} {
		if err := joinPressureNativeEvidence(encode(valid), bounds[0], bounds[1], 1); err == nil || !strings.Contains(err.Error(), "range") {
			t.Fatalf("bounds %v control: %v", bounds, err)
		}
	}
	if err := joinPressureNativeEvidence(encode(valid), 4, 48, 1); err == nil || !strings.Contains(err.Error(), "prefix arithmetic") {
		t.Fatalf("outside function control: %v", err)
	}
}
