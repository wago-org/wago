//go:build (linux || darwin || windows) && arm64 && !tinygo

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This exercises the direct synchronous host ABI. Dynamic public import
// wrappers use a different lowering and do not establish coverage of this path.
func syncResultPressureModule(t testing.TB, kind string, n int) (*wasm.Module, []wasm.ValType) {
	t.Helper()
	results := make([]wasm.ValType, n)
	slots := make([]int, n)
	slot := 0
	for i := range results {
		switch kind {
		case "gp":
			results[i] = wasm.I64
		case "fp":
			results[i] = wasm.F64
		case "fp32":
			results[i] = wasm.F32
		case "mixed":
			results[i] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128}[i%5]
		case "vector":
			results[i] = wasm.V128
		default:
			t.Fatalf("unknown kind %q", kind)
		}
		slots[i] = slot
		slot++
		if results[i] == wasm.V128 {
			slot++
		}
	}
	if slot > 64 {
		t.Fatalf("%d slots exceed ordinary host ABI", slot)
	}
	// Store each result separately. Distinct positions detect loss, lane swaps,
	// result reordering, and incorrect scalar widths. Leave eight-byte guards.
	body := []byte{5, 1, 0x7f, 1, 0x7e, 1, 0x7d, 1, 0x7c, 1, 0x7b}
	// Distinct integer and vector values remain live below the host results.
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(0x0123456789abcdef)...)
	body = append(body, 0xfd, 0x0c)
	body = binary.LittleEndian.AppendUint64(body, 0xfedcba9876543210)
	body = binary.LittleEndian.AppendUint64(body, 0x8877665544332211)
	body = append(body, 0x10, 0)
	for i := n - 1; i >= 0; i-- {
		local, store, align := byte(0), byte(0x36), byte(2)
		switch results[i] {
		case wasm.I64:
			local, store, align = 1, 0x37, 3
		case wasm.F32:
			local, store = 2, 0x38
		case wasm.F64:
			local, store, align = 3, 0x39, 3
		case wasm.V128:
			local, store, align = 4, 0x0b, 4
		}
		body = append(body, 0x21, local, 0x41)
		body = append(body, wasmtest.SLEB64(int64(8+slots[i]*8))...)
		body = append(body, 0x20, local)
		if results[i] == wasm.V128 {
			body = append(body, 0xfd)
		}
		body = append(body, store, align, 0)
	}
	body = append(body, 0x21, 4, 0x41)
	body = append(body, wasmtest.SLEB64(int64(8+slot*8))...)
	body = append(body, 0x20, 4, 0xfd, 0x0b, 4, 0, 0x21, 1, 0x41)
	body = append(body, wasmtest.SLEB64(int64(24+slot*8))...)
	body = append(body, 0x20, 1, 0x37, 3, 0, 0x41, 42, 0x0b)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("result")...), 0, 0)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m, results
}

func syncResultCompile(t testing.TB, m *wasm.Module, stackReg bool) []byte {
	t.Helper()
	var stats ModuleStats
	opts := CompileOptions{Workers: 1, SyncHostCalls: true, DeferCodeMapping: true, Optimizations: map[string]bool{"stack-reg": stackReg}}
	if diagnosticsEnabled {
		opts.Stats = &stats
	}
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if diagnosticsEnabled && (len(stats.Funcs) != 1 || stats.Funcs[0].Calls[callKindHostSync] != 1) {
		t.Fatalf("expected one direct synchronous host call: %+v", stats.Funcs)
	}
	// This module has one defined function, whose wrapper is at offset zero.
	if len(cm.Entry) != 1 || cm.Entry[0] != 0 {
		t.Fatalf("unexpected entries: %v", cm.Entry)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { cm.CodeImage.Close() })
	}
	return cm.Code
}

func TestSyncHostResultPressureCompile(t *testing.T) {
	for _, kind := range []string{"gp", "fp", "fp32", "mixed", "vector"} {
		for _, n := range []int{1, 8, 16, 32, 54, 64} {
			if n == 54 && kind != "mixed" || kind == "vector" && n > 32 || kind == "mixed" && n > 54 {
				continue
			}
			for _, stackReg := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/stack-reg=%t", kind, n, stackReg), func(t *testing.T) { m, _ := syncResultPressureModule(t, kind, n); syncResultCompile(t, m, stackReg) })
			}
		}
	}
}

// The callback and oracle share only raw ABI slots. The Wasm stores and their
// widths are checked independently against a byte image, including all guards.
func syncResultExecutor(t testing.TB, code []byte, types []wasm.ValType) (func(), func(uint64), func()) {
	t.Helper()
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { jm.Close() })
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ar.Close() })
	mapped, entry, err := runtime.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Unmap(mapped) })
	if !bytes.Equal(mapped[:len(code)], code) {
		t.Fatal("mapped code differs")
	}
	args, out, trap, ctrl := ar.Alloc(8), ar.Alloc(8), ar.Alloc(runtime.TrapBufferBytes), ar.Alloc(runtime.HostCtrlFrameBytes)
	jm.SetCustomCtx(uintptr(unsafe.Pointer(&ctrl[0])))
	slots := len(types)
	for _, typ := range types {
		if typ == wasm.V128 {
			slots++
		}
	}
	raw := make([]uint64, slots)
	want := make([]byte, 40+slots*8)
	calls, runs := 0, 0
	host := func(_ uintptr, index uint32, args, results []uint64) {
		if index != 0 || len(args) != 0 || len(results) != slots {
			t.Fatalf("host signature: index=%d args=%d results=%d", index, len(args), len(results))
		}
		copy(results, raw)
		calls++
	}
	set := func(seed uint64) {
		for i := range want {
			want[i] = 0xa5
		}
		for i := range raw {
			raw[i] = 0xfff800007fc00000 ^ uint64(i+1)*0x010305070b0d1113 ^ seed
		}
		slot := 0
		for _, typ := range types {
			off := 8 + slot*8
			switch typ {
			case wasm.I32, wasm.F32:
				binary.LittleEndian.PutUint32(want[off:], uint32(raw[slot]))
			case wasm.V128:
				binary.LittleEndian.PutUint64(want[off:], raw[slot])
				binary.LittleEndian.PutUint64(want[off+8:], raw[slot+1])
				slot++
			default:
				binary.LittleEndian.PutUint64(want[off:], raw[slot])
			}
			slot++
		}
		binary.LittleEndian.PutUint64(want[8+slots*8:], 0xfedcba9876543210)
		binary.LittleEndian.PutUint64(want[16+slots*8:], 0x8877665544332211)
		binary.LittleEndian.PutUint64(want[24+slots*8:], 0x0123456789abcdef)
		for i := range want {
			jm.CurrentBytes()[i] = 0xa5
		}
		calls, runs = 0, 0
	}
	run := func() {
		runs++
		if err := eng.CallWithHost(entry, args, jm.LinearMemory(), trap, out, ctrl, host); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		if calls != runs || runs == 0 || binary.LittleEndian.Uint32(out) != 42 {
			t.Fatalf("completion: calls=%d result=%x", calls, out)
		}
		if got := jm.CurrentBytes()[:len(want)]; !bytes.Equal(got, want) {
			t.Fatalf("result image:\ngot  %x\nwant %x", got, want)
		}
	}
	return run, set, check
}

func TestSyncHostResultPressureExecution(t *testing.T) {
	for _, kind := range []string{"gp", "fp", "fp32", "mixed", "vector"} {
		for _, n := range []int{1, 8, 16, 32, 54, 64} {
			if n == 54 && kind != "mixed" || kind == "vector" && n > 32 || kind == "mixed" && n > 54 {
				continue
			}
			for _, stackReg := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/stack-reg=%t", kind, n, stackReg), func(t *testing.T) {
					m, types := syncResultPressureModule(t, kind, n)
					code := syncResultCompile(t, m, stackReg)
					run, set, check := syncResultExecutor(t, code, types)
					for _, seed := range []uint64{0, 0xffffffffffffffff, 0x0123456789abcdef, 0xfff800007fc00000 ^ 0x010305070b0d1113 ^ 0x8000000000000000, 0xfff800007fc00000 ^ 0x010305070b0d1113 ^ 0x7ff8000000000042, 0xfff800007fc00000 ^ 0x010305070b0d1113 ^ 0x80000000, 0xfff800007fc00000 ^ 0x010305070b0d1113 ^ 0x7fc00042} {
						set(seed)
						run()
						check()
					}
				})
			}
		}
	}
}

func BenchmarkSyncHostResults(b *testing.B) {
	for _, kind := range []string{"gp", "fp", "mixed", "vector"} {
		// One-result calls are valid before and after the fix. Wider signatures are
		// correctness cases; do not execute the known-bad compiler output as a baseline.
		b.Run(kind, func(b *testing.B) {
			m, types := syncResultPressureModule(b, kind, 1)
			b.Run("compile", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					syncResultCompile(b, m, true)
				}
			})
			b.Run("execute", func(b *testing.B) {
				code := syncResultCompile(b, m, true)
				run, set, check := syncResultExecutor(b, code, types)
				set(0)
				run()
				check()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					run()
				}
				b.StopTimer()
				check()
			})
		})
	}
}
