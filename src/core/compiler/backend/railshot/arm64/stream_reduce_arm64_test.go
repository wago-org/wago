//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func streamingReductionModule(t testing.TB, mutate bool) *wasm.Module {
	body := []byte{0}
	for i := byte(0); i < 6; i++ {
		body = append(body, 0x41, 0, 0x41, i+1, 0x36, 2, i*4)
	}
	for i := byte(0); i < 6; i++ {
		body = append(body, 0x20, 0)
		if mutate {
			body = append(body, 0x41, 1, 0x6a, 0x22, 0)
		}
		body = append(body, 0x41, 0, 0x28, 2, i*4, 0x6c)
	}
	for i := 0; i < 5; i++ {
		body = append(body, 0x6a)
	}
	if mutate {
		body = append(body, 0x20, 0, 0x6a)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestStreamingReductionOverflowAndLocalWrites(t *testing.T) {
	old := streamReduceEnabled
	defer func() { streamReduceEnabled = old }()
	for _, mutate := range []bool{false, true} {
		m := streamingReductionModule(t, mutate)
		for _, enabled := range []bool{false, true} {
			streamReduceEnabled = enabled
			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 1, 17, 0x7fffffff, 0x80000000, 0xffffffff} {
					want := x * 21
					if mutate {
						want = x*22 + 97
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, x)
					if err != nil || uint32(got) != uint32(want) {
						t.Fatalf("mutate=%v enabled=%v guard=%v x=%x got=%x want=%x err=%v", mutate, enabled, guard, x, got, want, err)
					}
				}
			}
		}
	}
}

func TestStreamingReductionAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := streamReduceEnabled
	defer func() { streamReduceEnabled = old }()
	for _, mutate := range []bool{false, true} {
		m := streamingReductionModule(t, mutate)
		for _, enabled := range []bool{false, true} {
			streamReduceEnabled = enabled
			hits := compileWithStats(t, m, true).Funcs[0].Peephole["streaming-integer-reduction"]
			if enabled && hits != 1 || !enabled && hits != 0 {
				t.Fatalf("mutate=%v enabled=%v hits=%d", mutate, enabled, hits)
			}
		}
	}
}

func TestStreamingReductionRejectsCompletedTermWrites(t *testing.T) {
	f := fn{localType: []machineType{mtI32}}
	makeCode := func(post byte) []byte {
		var b []byte
		for i := 0; i < 4; i++ {
			b = append(b, 0x20, 0, 0x41, 1, 0x6c)
			if i == 0 && post != 0 {
				b = append(b, post, 0)
			}
		}
		b = append(b, 0x6a, 0x6a, 0x6a)
		return b
	}
	for _, post := range []byte{0, 0x21, 0x22} {
		r := wasm.ReaderFrom(makeCode(post))
		_, ok := f.inspectStreamReduction(&r)
		if ok != (post == 0) {
			t.Fatalf("post=%x admitted=%v", post, ok)
		}
	}
	f.localType[0] = mtI64
	r := wasm.ReaderFrom(makeCode(0))
	if _, ok := f.inspectStreamReduction(&r); ok {
		t.Fatal("admitted non-i32 local")
	}
}

func TestStreamingReductionManyLiveLocals(t *testing.T) {
	old := streamReduceEnabled
	defer func() { streamReduceEnabled = old }()
	for _, terms := range []byte{16, 32, 64} {
		body := []byte{1, terms, 0x7f}
		for i := byte(1); i <= terms; i++ {
			body = append(body, 0x41, 0, 0x41)
			body = append(body, wasmtest.SLEB32(int32(i))...)
			body = append(body, 0x36, 2)
			body = append(body, wasmtest.ULEB(uint32(i-1)*4)...)
		}
		for i := byte(1); i <= terms; i++ {
			body = append(body, 0x41, 0, 0x28, 2)
			body = append(body, wasmtest.ULEB(uint32(i-1)*4)...)
			body = append(body, 0x21, i)
		}
		body = append(body, 0x03, 0x7f)
		for i := byte(1); i <= terms; i++ {
			body = append(body, 0x20, i, 0x20, 0, 0x6c)
		}
		for i := byte(1); i < terms; i++ {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0b, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, enabled := range []bool{false, true} {
			streamReduceEnabled = enabled
			for _, guard := range []bool{false, true} {
				for _, x := range []uint64{0, 17, 0xffffffff} {
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, x)
					want := x * uint64(terms) * uint64(terms+1) / 2
					if err != nil || uint32(got) != uint32(want) {
						t.Fatalf("terms=%d enabled=%v guard=%v x=%x got=%x want=%x err=%v", terms, enabled, guard, x, got, want, err)
					}
				}
			}
		}
	}
}

func TestStreamingReductionPolicyRollback(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := streamReduceEnabled
	defer func() { streamReduceEnabled = old }()
	streamReduceEnabled = true
	m := streamingReductionModule(t, true)
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, CompileOptions{Stats: stats, Optimizations: map[string]bool{"streaming-i32-reduction": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		cm.CodeImage.Close()
	}
	if stats.Funcs[0].Peephole["streaming-integer-reduction"] != 0 {
		t.Fatal("policy rollback ignored")
	}
}
